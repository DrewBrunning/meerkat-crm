package controllers

import (
	"errors"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// thinContactIsPet reports whether a thin contact being created at one end of
// a relationship edge is the pet side of a pet/owner pair. Per the type
// registry (models/relationship_type_registry.go), "A owned_by B" reads "A is
// owned by B" — so the pet is the SOURCE of an owned_by edge, and by symmetry
// the TARGET of an owns edge. Any other combination is the owner (presumably
// human) and must not be classified as an animal.
func thinContactIsPet(edgeType string, isSource bool) bool {
	if isSource {
		return edgeType == "owned_by"
	}
	return edgeType == "owns"
}

// resolveRelationshipEndpoint resolves one RelationshipEdge endpoint (source
// or target) to a Contact.VCardUID: either verifying ownership of an
// existing contact (id != ""), or creating a new "thin" Contact from thin
// (mirroring cmd/backfill-relationship-edges/migrate.go's field mapping --
// Firstname=thin.Name verbatim, Gender/Birthday passed through). Returns a
// *apperrors.AppError rather than aborting directly (like verifyOwnedContact
// does, life_event_controller.go) because this runs inside a
// db.Transaction closure and must not abort mid-transaction -- the caller
// aborts exactly once, after the transaction resolves, preserving the
// specific NotFound/InvalidInput status rather than flattening every
// failure into a generic 500.
//
// edgeType/isSource drive T37's thin-contact-kind defaulting: a thin contact
// that is the pet side of an owned_by/owns pair (see thinContactIsPet) gets
// CRM.Kind = "animal", so the household-suggestion engine
// (services.household_service.go's classifyMember) doesn't silently treat a
// just-created pet as a human adult until someone fixes its kind by hand.
// Set through ApplyRecordToContact (models/contact_record_reverse.go), never
// by mutating the struct field directly — BeforeSave re-derives
// Card/CRM/Passthrough from the flat fields on Create and would discard a
// direct field mutation (CLAUDE.md backend trap 2).
func resolveRelationshipEndpoint(tx *gorm.DB, userID uint, fieldName, id string, thin *models.ThinContactInput, edgeType string, isSource bool) (string, *apperrors.AppError) {
	switch {
	case id != "" && thin != nil:
		return "", apperrors.ErrInvalidInput(fieldName, "specify either an existing contact id or a new contact, not both")
	case id != "":
		var contact models.Contact
		if err := tx.Where("vcard_uid = ? AND user_id = ?", id, userID).First(&contact).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", apperrors.ErrNotFound("Contact").WithDetails(fieldName, id)
			}
			return "", apperrors.ErrDatabase("Failed to retrieve contact").WithError(err)
		}
		return contact.VCardUID, nil
	case thin != nil:
		contact := models.Contact{UserID: userID, Firstname: thin.Name, Gender: thin.Gender, Birthday: thin.Birthday}
		if thinContactIsPet(edgeType, isSource) {
			// Round-trip through the neutral Record so the kind rides the
			// authoritative c.CRM path (ApplyRecordToContact sets the
			// cardSetDirectly guard that keeps BeforeSave from re-deriving
			// and discarding it), rather than mutating c.CRM.Kind directly.
			record := models.RecordFromContact(&contact, models.DefaultPhotoDir)
			record.Envelope.Kind = "animal"
			models.ApplyRecordToContact(&contact, record, "")
		}
		if err := tx.Create(&contact).Error; err != nil {
			return "", apperrors.ErrDatabase("Failed to create related contact").WithError(err)
		}
		return contact.VCardUID, nil
	default:
		return "", apperrors.ErrInvalidInput(fieldName, "must specify either an existing contact id or a new contact")
	}
}

// applyRelationshipEdgeInput resolves both endpoints (creating thin contacts
// as needed) and writes Type/Directional/Metadata/Sensitivity onto edge,
// inside a transaction so a thin-contact insert and the edge write/update
// commit or roll back together. Used by both CreateRelationshipEdge and
// UpdateRelationshipEdge. edge.UserID/Status/Source/Confidence must already
// be set by the caller before calling this (create: fresh values; update:
// the pre-loaded row's existing values, left untouched).
func applyRelationshipEdgeInput(db *gorm.DB, userID uint, edge *models.RelationshipEdge, input *models.RelationshipEdgeInput) error {
	sensitivity := input.Sensitivity
	if sensitivity == "" {
		sensitivity = models.RelationshipSensitivityNormal
	}

	return db.Transaction(func(tx *gorm.DB) error {
		sourceID, aerr := resolveRelationshipEndpoint(tx, userID, "source_id", input.SourceID, input.SourceThin, input.Type, true)
		if aerr != nil {
			return aerr
		}
		targetID, aerr := resolveRelationshipEndpoint(tx, userID, "target_id", input.TargetID, input.TargetThin, input.Type, false)
		if aerr != nil {
			return aerr
		}
		if sourceID == targetID {
			return apperrors.ErrInvalidInput("target_id", "source and target must be different contacts")
		}

		edge.SourceID = sourceID
		edge.TargetID = targetID
		edge.Type = input.Type
		edge.Directional = !models.IsSymmetricRelationType(input.Type)
		edge.Metadata = input.Metadata
		edge.Sensitivity = sensitivity

		// Natural-key duplicate gate (issue #928). An edge is identified by
		// (user, source, target, type); a double-clicked or replayed create
		// must not insert the fact twice. This check is the clear 409; the
		// unique index added by migration 000055 is the race-proof backstop
		// and its violation is mapped to the same 409 below. On update the
		// edge is excluded from its own lookup, but re-pointing an edge onto
		// an existing tuple is the same duplicate.
		if aerr := relationshipEdgeDuplicate(tx, edge); aerr != nil {
			return aerr
		}

		if edge.ID == "" {
			if err := tx.Create(edge).Error; err != nil {
				return relationshipEdgeWriteError(err)
			}
			return nil
		}
		if err := tx.Save(edge).Error; err != nil {
			return relationshipEdgeWriteError(err)
		}
		return nil
	})
}

// relationshipEdgeDuplicate returns a 409 AppError when an edge with the same
// natural key (user, source, target, type) already exists. On update the edge
// under edit is excluded from the lookup so re-saving it is not a duplicate.
func relationshipEdgeDuplicate(tx *gorm.DB, edge *models.RelationshipEdge) *apperrors.AppError {
	q := tx.Model(&models.RelationshipEdge{}).Where(
		"user_id = ? AND source_id = ? AND target_id = ? AND type = ?",
		edge.UserID, edge.SourceID, edge.TargetID, edge.Type,
	)
	if edge.ID != "" {
		q = q.Where("id != ?", edge.ID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return apperrors.ErrDatabase("Failed to check for an existing relationship edge").WithError(err)
	}
	if count > 0 {
		return apperrors.ErrAlreadyExists("Relationship edge")
	}
	return nil
}

// relationshipEdgeWriteError maps a SQLite unique-constraint violation on the
// relationship_edges natural key (migration 000055) to the checked 409
// ErrAlreadyExists path. Two concurrent POSTs can both pass
// relationshipEdgeDuplicate's SELECT before either INSERT lands; the unique
// index is what actually serializes them, and this keeps the loser a 409
// rather than a generic 500.
func relationshipEdgeWriteError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed: relationship_edges") {
		return apperrors.ErrAlreadyExists("Relationship edge").WithError(err)
	}
	return err
}

// abortRelationshipEdgeError unwraps a possibly-*apperrors.AppError coming
// out of applyRelationshipEdgeInput's transaction and aborts with the
// specific status (404/400) when present, falling back to a generic 500
// database error otherwise -- unlike a pure cascade-delete, this
// transaction's failures legitimately include 404 (endpoint contact not
// found) and 400 (bad source/target combo) cases that must not be
// flattened into a 500.
func abortRelationshipEdgeError(c *gin.Context, err error, fallback string) {
	if appErr, ok := err.(*apperrors.AppError); ok {
		apperrors.AbortWithError(c, appErr)
		return
	}
	apperrors.AbortWithError(c, apperrors.ErrDatabase(fallback).WithError(err))
}

// CreateRelationshipEdge creates a new RelationshipEdge for the authenticated
// user. Source/Confidence/Status are server-derived, never client-supplied:
// a user-created edge is always Source: user-confirmed, Confidence: 1.0,
// Status: confirmed (see RelationshipEdgeInput's doc comment).
func CreateRelationshipEdge(c *gin.Context) {
	input, verr := middleware.GetValidated[models.RelationshipEdgeInput](c)
	if verr != nil {
		apperrors.AbortWithError(c, verr)
		return
	}

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	edge := models.RelationshipEdge{
		UserID:     userID,
		Source:     models.RelationshipSourceUserConfirmed,
		Confidence: 1.0,
		Status:     models.RelationshipStatusConfirmed,
	}
	if err := applyRelationshipEdgeInput(db, userID, &edge, input); err != nil {
		abortRelationshipEdgeError(c, err, "Failed to save relationship edge")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"relationship_edge": edge})
}

// GetRelationshipEdge returns one RelationshipEdge.
func GetRelationshipEdge(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var edge models.RelationshipEdge
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&edge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Relationship edge").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve relationship edge").WithError(err))
		}
		return
	}

	c.JSON(http.StatusOK, edge)
}

// ListRelationshipEdges returns the authenticated user's RelationshipEdges,
// paginated, optionally filtered by ?contact_id=<Contact.VCardUID> (matches
// either SourceID or TargetID -- the "both directions in one call" ask that
// replaces the legacy GetRelationships/GetIncomingRelationships split),
// ?status=confirmed|suggested (the suggestion-review-inbox query -- omit
// contact_id to page through every pending suggestion across all contacts),
// and ?type=<registered token>.
func ListRelationshipEdges(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	params, err := GetCursorParams(c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}
	contactID := c.Query("contact_id")
	status := c.Query("status")
	relType := c.Query("type")

	baseQuery := db.Model(&models.RelationshipEdge{}).Where("user_id = ?", userID)
	if contactID != "" {
		// Parens are load-bearing: an unparenthesized OR chained after the
		// user_id clause would let it bind looser than intended and leak
		// other users' edges. Mirrors the exact safe pattern already used
		// for this same OR (contact_controller.go's DeleteContact,
		// "(source_id = ? OR target_id = ?) AND user_id = ?").
		baseQuery = baseQuery.Where("(source_id = ? OR target_id = ?)", contactID, contactID)
	}
	if status != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}
	if relType != "" {
		baseQuery = baseQuery.Where("type = ?", relType)
	}

	var total int64
	if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to count relationship edges").WithError(err))
		return
	}

	desc := params.Order == "desc"
	if params.Cursor != nil {
		pred, t, idv := cursorPredicate("relationship_edges", params.Cursor, params.Cursor.ID, desc)
		baseQuery = baseQuery.Where(pred, t, idv)
	}

	var edges []models.RelationshipEdge
	if err := cursorOrderBy(baseQuery, "relationship_edges", desc).
		Limit(params.Limit + 1).
		Find(&edges).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve relationship edges").WithError(err))
		return
	}
	nextCursor := ""
	if len(edges) > params.Limit {
		edges = edges[:params.Limit]
		nextCursor = EncodeCursor(edges[len(edges)-1].UpdatedAt, edges[len(edges)-1].ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"relationship_edges": edges,
		"total":              total,
		"next_cursor":        nextCursor,
		"limit":              params.Limit,
		"sync":               buildSyncMeta(SyncModeFullResync),
	})
}

// UpdateRelationshipEdge replaces a RelationshipEdge's editable fields
// (SourceID/TargetID and their thin-creation alternatives, Type, Metadata,
// Sensitivity) -- full-replace semantics via the same RelationshipEdgeInput
// as create, matching UpdateLifeEvent's own precedent of treating every
// field in the shared Input DTO as replaceable, including which contact(s)
// the record points at. Status/Source/Confidence are untouched (they are
// not part of RelationshipEdgeInput at all) -- use AcceptRelationshipEdge to
// change Status.
func UpdateRelationshipEdge(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var edge models.RelationshipEdge
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&edge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Relationship edge").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve relationship edge").WithError(err))
		}
		return
	}

	input, verr := middleware.GetValidated[models.RelationshipEdgeInput](c)
	if verr != nil {
		apperrors.AbortWithError(c, verr)
		return
	}

	if err := applyRelationshipEdgeInput(db, userID, &edge, input); err != nil {
		abortRelationshipEdgeError(c, err, "Failed to update relationship edge")
		return
	}

	c.JSON(http.StatusOK, edge)
}

// DeleteRelationshipEdge deletes a RelationshipEdge. For a Status: suggested
// edge this doubles as "reject" -- there is deliberately no separate reject
// endpoint (see AcceptRelationshipEdge's doc comment for the accept/reject
// asymmetry rationale).
func DeleteRelationshipEdge(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var edge models.RelationshipEdge
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&edge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Relationship edge").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve relationship edge").WithError(err))
		}
		return
	}

	if err := db.Delete(&edge).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete relationship edge").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Relationship edge deleted"})
}

// AcceptRelationshipEdge promotes a Status: suggested edge to confirmed -- the
// only consumer the household- and graph-inference engines'
// (services.GenerateHouseholdSuggestions / GenerateGraphSuggestions) suggested
// edges have anywhere in the app today. A dedicated endpoint rather than
// folding this into UpdateRelationshipEdge's PUT: PUT is full-replace and
// requires resending Type/Sensitivity/Metadata/SourceID/TargetID, which is
// both bad ergonomics for a single-field transition and a real hazard -- a
// client trying to "just accept" would silently be able to edit the
// type/sensitivity/endpoints in the same call. Source/Confidence are left
// untouched deliberately: accepting only changes authority (Status), not
// provenance -- the edge should keep showing it originated from inference
// even once accepted. Rejection has no equivalent dedicated endpoint: it's
// just DELETE (see DeleteRelationshipEdge).
func AcceptRelationshipEdge(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var edge models.RelationshipEdge
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&edge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Relationship edge").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve relationship edge").WithError(err))
		}
		return
	}

	if edge.Status != models.RelationshipStatusSuggested {
		apperrors.AbortWithError(c, apperrors.ErrConflict("Relationship edge is not a pending suggestion"))
		return
	}

	edge.Status = models.RelationshipStatusConfirmed
	if err := db.Save(&edge).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to accept relationship edge").WithError(err))
		return
	}

	c.JSON(http.StatusOK, edge)
}

// SuggestRelationshipEdges is the T104 trigger: run one round of graph
// inference (services.GenerateGraphSuggestions) over the caller's confirmed
// edges and persist any newly-derived suggestions. Ownership-scoped to the
// authenticated user by construction — the engine loads and writes only the
// caller's own edges. Opt-in (the user presses the button), one round per
// press, and idempotent: re-running never duplicates, exactly like the
// household engine.
func SuggestRelationshipEdges(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	created, err := services.GenerateGraphSuggestions(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to generate relationship suggestions").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "Relationship suggestions generated",
		"suggested_edges": created,
		"total":           len(created),
	})
}
