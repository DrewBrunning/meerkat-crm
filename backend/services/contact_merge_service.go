package services

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"mycorrhizal/models"

	"gorm.io/gorm"
)

// mergeScalarFields is the field table backing both ComputeContactMergeResolution
// and ApplyContactMergeResolution: every scalar covered by ticket N1's
// "Resolution by field class" table (N1).
// Deliberately excludes Email/Phone/Address: those are legacy scalars
// Contact.BeforeSave (models/contact.go) derives from Emails[0]/Phones[0]/
// Addresses[0] on every save -- resolving them here and then letting
// BeforeSave immediately overwrite them again would be silently
// self-defeating.
type scalarFieldSpec struct {
	Key   string
	Label string
	Get   func(c *models.Contact) string
	Set   func(c *models.Contact, v string)
}

var mergeScalarFields = []scalarFieldSpec{
	{"firstname", "First Name", func(c *models.Contact) string { return c.Firstname }, func(c *models.Contact, v string) { c.Firstname = v }},
	{"lastname", "Last Name", func(c *models.Contact) string { return c.Lastname }, func(c *models.Contact, v string) { c.Lastname = v }},
	{"middle_name", "Middle Name", func(c *models.Contact) string { return c.MiddleName }, func(c *models.Contact, v string) { c.MiddleName = v }},
	{"prefix", "Prefix", func(c *models.Contact) string { return c.Prefix }, func(c *models.Contact, v string) { c.Prefix = v }},
	{"suffix", "Suffix", func(c *models.Contact) string { return c.Suffix }, func(c *models.Contact, v string) { c.Suffix = v }},
	{"nickname", "Nickname", func(c *models.Contact) string { return c.Nickname }, func(c *models.Contact, v string) { c.Nickname = v }},
	{"gender", "Gender", func(c *models.Contact) string { return c.Gender }, func(c *models.Contact, v string) { c.Gender = v }},
	{"birthday", "Birthday", func(c *models.Contact) string { return c.Birthday }, func(c *models.Contact, v string) { c.Birthday = v }},
	{"anniversary", "Anniversary", func(c *models.Contact) string { return c.Anniversary }, func(c *models.Contact, v string) { c.Anniversary = v }},
	{"organization", "Organization", func(c *models.Contact) string { return c.Organization }, func(c *models.Contact, v string) { c.Organization = v }},
	{"department", "Department", func(c *models.Contact) string { return c.Department }, func(c *models.Contact, v string) { c.Department = v }},
	{"job_title", "Job Title", func(c *models.Contact) string { return c.JobTitle }, func(c *models.Contact, v string) { c.JobTitle = v }},
	{"role", "Role", func(c *models.Contact) string { return c.Role }, func(c *models.Contact, v string) { c.Role = v }},
	{"how_we_met", "How We Met", func(c *models.Contact) string { return c.HowWeMet }, func(c *models.Contact, v string) { c.HowWeMet = v }},
	{"work_information", "Work Information", func(c *models.Contact) string { return c.WorkInformation }, func(c *models.Contact, v string) { c.WorkInformation = v }},
	{"contact_information", "Contact Information", func(c *models.Contact) string { return c.ContactInformation }, func(c *models.Contact, v string) { c.ContactInformation = v }},
}

// ComputeContactMergeResolution is pure (no DB I/O). Shared by the preview
// and commit controllers so they can never compute a different answer for
// the same pair.
func ComputeContactMergeResolution(keeper, loser *models.Contact) *models.ContactMergeResolution {
	res := &models.ContactMergeResolution{ResolvedScalars: map[string]string{}}
	for _, f := range mergeScalarFields {
		kv, lv := f.Get(keeper), f.Get(loser)
		switch {
		case kv == lv:
			res.ResolvedScalars[f.Key] = kv
		case kv == "":
			res.ResolvedScalars[f.Key] = lv
		case lv == "":
			res.ResolvedScalars[f.Key] = kv
		default:
			res.Conflicts = append(res.Conflicts, models.ContactMergeFieldConflict{
				Field: f.Key, Label: f.Label, KeeperValue: kv, LoserValue: lv,
			})
		}
	}
	sort.Slice(res.Conflicts, func(i, j int) bool { return res.Conflicts[i].Field < res.Conflicts[j].Field })

	res.Emails = unionEmails(keeper.Emails, loser.Emails)
	res.Phones = unionPhones(keeper.Phones, loser.Phones)
	res.Addresses = unionAddresses(keeper.Addresses, loser.Addresses)
	res.URLs = unionURLs(keeper.URLs, loser.URLs)
	res.IMPPs = unionIMPPs(keeper.IMPPs, loser.IMPPs)
	res.Circles = unionCircles(keeper.Circles, loser.Circles)
	return res
}

// ApplyContactMergeResolution mutates keeper in place: every auto-resolved
// scalar, every unioned multi-valued/Circles field from res,
// plus (for the conflict set) the caller-supplied resolutions. Does not call
// db.Save -- the caller does that, inside its own transaction. Returns an
// error naming the first unresolved conflict field; the controller is
// expected to have already validated this (see contact_merge_controller.go),
// so this is a defense-in-depth check, not the primary rejection path.
func ApplyContactMergeResolution(keeper *models.Contact, res *models.ContactMergeResolution, resolutions map[string]string) error {
	for _, f := range mergeScalarFields {
		if v, ok := res.ResolvedScalars[f.Key]; ok {
			f.Set(keeper, v)
		}
	}
	for _, conflict := range res.Conflicts {
		v, ok := resolutions[conflict.Field]
		if !ok {
			return fmt.Errorf("contact merge: unresolved conflict for field %q", conflict.Field)
		}
		// T107: conflict.Field == cadencePolicyConflictField ("cadence_policy")
		// deliberately matches no mergeScalarFields entry and is a no-op here
		// -- repointCadencePolicy applies that resolution separately, since a
		// CadencePolicy isn't a Contact field this function can Set(). This
		// only stays correct because cadencePolicyConflictField never
		// collides with a real mergeScalarFields key; if a future scalar
		// field is ever added under that same name, it would silently start
		// receiving the cadence conflict's chosen value instead.
		for _, f := range mergeScalarFields {
			if f.Key == conflict.Field {
				f.Set(keeper, v)
			}
		}
	}
	keeper.Emails = res.Emails
	keeper.Phones = res.Phones
	keeper.Addresses = res.Addresses
	keeper.URLs = res.URLs
	keeper.IMPPs = res.IMPPs
	keeper.Circles = res.Circles
	return nil
}

// unionEmails unions a and b, de-duplicating on (case-insensitive type,
// case-insensitive value). Keeper's entries (a) are added first so its
// casing/order wins ties.
func unionEmails(a, b []models.ContactEmail) []models.ContactEmail {
	seen := map[string]bool{}
	var out []models.ContactEmail
	add := func(e models.ContactEmail) {
		key := strings.ToLower(strings.TrimSpace(e.Type)) + "|" + strings.ToLower(strings.TrimSpace(e.Value))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, e)
	}
	for _, e := range a {
		add(e)
	}
	for _, e := range b {
		add(e)
	}
	return out
}

// unionPhones unions on (case-insensitive type, canonical phone key) -- reuses
// models.PhoneKey so "phone equality" means the same thing here as it already
// does for DetectDuplicate's phone-match path.
func unionPhones(a, b []models.ContactPhone) []models.ContactPhone {
	seen := map[string]bool{}
	var out []models.ContactPhone
	add := func(p models.ContactPhone) {
		phoneKey := models.PhoneKey(p.Value)
		if phoneKey == "" {
			out = append(out, p)
			return
		}
		key := strings.ToLower(strings.TrimSpace(p.Type)) + "|" + phoneKey
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	for _, p := range a {
		add(p)
	}
	for _, p := range b {
		add(p)
	}
	return out
}

// unionURLs dedups on (case-insensitive type, case-insensitive value).
func unionURLs(a, b []models.ContactURL) []models.ContactURL {
	seen := map[string]bool{}
	var out []models.ContactURL
	add := func(u models.ContactURL) {
		key := strings.ToLower(strings.TrimSpace(u.Type)) + "|" + strings.ToLower(strings.TrimSpace(u.Value))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, u)
	}
	for _, u := range a {
		add(u)
	}
	for _, u := range b {
		add(u)
	}
	return out
}

// unionIMPPs dedups on (case-insensitive type, case-insensitive value).
func unionIMPPs(a, b []models.ContactIMPP) []models.ContactIMPP {
	seen := map[string]bool{}
	var out []models.ContactIMPP
	add := func(i models.ContactIMPP) {
		key := strings.ToLower(strings.TrimSpace(i.Type)) + "|" + strings.ToLower(strings.TrimSpace(i.Value))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, i)
	}
	for _, i := range a {
		add(i)
	}
	for _, i := range b {
		add(i)
	}
	return out
}

// unionAddresses dedups on every field lowercased+trimmed -- a postal
// address has no single "value" to normalize on; its identity is the whole
// tuple. The T79 sub-street fields (PO box / apartment / floor) are part of
// that tuple: two addresses that differ only in apartment are genuinely
// different, and since T79 gave the flat struct a slot for them, collapsing
// them here would be real data loss the projection can now prevent.
func unionAddresses(a, b []models.ContactAddress) []models.ContactAddress {
	seen := map[string]bool{}
	var out []models.ContactAddress
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	add := func(addr models.ContactAddress) {
		key := strings.Join([]string{norm(addr.Type), norm(addr.Street), norm(addr.POBox), norm(addr.Apartment), norm(addr.Floor), norm(addr.City), norm(addr.Region), norm(addr.Postal), norm(addr.Country)}, "|")
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, addr)
	}
	for _, addr := range a {
		add(addr)
	}
	for _, addr := range b {
		add(addr)
	}
	return out
}

// unionCircles unions with case-insensitive dedup, keeping the keeper's
// casing on a tie (a appears first).
func unionCircles(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		key := strings.ToLower(strings.TrimSpace(s))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range a {
		add(s)
	}
	for _, s := range b {
		add(s)
	}
	return out
}

// ComputeFieldValueConflicts finds every FieldDefinition where both keeper
// and loser have a FieldValue row -- these cannot be unioned (the schema
// allows only one value per field per contact, unlike every other
// association type), so each is a conflict the user must resolve, keyed by
// FieldDefinition.ID rather than a fixed field name.
func ComputeFieldValueConflicts(db *gorm.DB, userID uint, keeperVCardUID, loserVCardUID string) ([]models.ContactMergeFieldConflict, error) {
	var keeperValues, loserValues []models.FieldValue
	if err := db.Where("entity_id = ? AND user_id = ?", keeperVCardUID, userID).Find(&keeperValues).Error; err != nil {
		return nil, err
	}
	if err := db.Where("entity_id = ? AND user_id = ?", loserVCardUID, userID).Find(&loserValues).Error; err != nil {
		return nil, err
	}
	if len(keeperValues) == 0 || len(loserValues) == 0 {
		return nil, nil
	}

	keeperByDef := make(map[string]models.FieldValue, len(keeperValues))
	for _, v := range keeperValues {
		keeperByDef[v.FieldDefinitionID] = v
	}

	var conflicts []models.ContactMergeFieldConflict
	var defIDs []string
	loserByDef := make(map[string]models.FieldValue, len(loserValues))
	for _, v := range loserValues {
		if _, ok := keeperByDef[v.FieldDefinitionID]; ok {
			loserByDef[v.FieldDefinitionID] = v
			defIDs = append(defIDs, v.FieldDefinitionID)
		}
	}
	if len(defIDs) == 0 {
		return nil, nil
	}

	var defs []models.FieldDefinition
	if err := db.Where("id IN ? AND user_id = ?", defIDs, userID).Find(&defs).Error; err != nil {
		return nil, err
	}
	labelByID := make(map[string]string, len(defs))
	for _, d := range defs {
		labelByID[d.ID] = d.Label
	}

	for _, defID := range defIDs {
		conflicts = append(conflicts, models.ContactMergeFieldConflict{
			Field:       defID,
			Label:       labelByID[defID],
			KeeperValue: string(keeperByDef[defID].Value),
			LoserValue:  string(loserByDef[defID].Value),
		})
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Field < conflicts[j].Field })
	return conflicts, nil
}

// ComputeContactMergeAssociationCounts runs the COUNT(*) queries the preview
// endpoint returns and the commit path snapshots (pre-repoint) for the audit
// note. db may be either the plain *gorm.DB (preview, read-only) or a
// *gorm.DB transaction handle (commit, so the counts reflect the same
// row-visibility as the repoint that follows).
func ComputeContactMergeAssociationCounts(db *gorm.DB, userID uint, loserID uint, loserVCardUID string) (models.ContactMergeAssociationCounts, error) {
	var c models.ContactMergeAssociationCounts
	steps := []struct {
		dst *int64
		q   *gorm.DB
	}{
		{&c.Notes, db.Model(&models.Note{}).Where("contact_id = ? AND user_id = ?", loserID, userID)},
		{&c.Reminders, db.Model(&models.Reminder{}).Where("contact_id = ? AND user_id = ?", loserID, userID)},
		{&c.ReminderCompletions, db.Model(&models.ReminderCompletion{}).Where("contact_id = ? AND user_id = ?", loserID, userID)},
		{&c.RelationshipEdges, db.Model(&models.RelationshipEdge{}).Where("user_id = ? AND (source_id = ? OR target_id = ?)", userID, loserVCardUID, loserVCardUID)},
		{&c.HouseholdMemberships, db.Model(&models.HouseholdMember{}).Where("member_vcard_uid = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.CircleMemberships, db.Model(&models.CircleMember{}).Where("member_vcard_uid = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.Tags, db.Model(&models.ContactTag{}).Where("contact_vcard_uid = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.LifeEvents, db.Model(&models.LifeEvent{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.LifeEventReferences, db.Model(&models.LifeEvent{}).Where(
			"user_id = ? AND related_entity_ids IS NOT NULL AND EXISTS "+
				"(SELECT 1 FROM json_each(life_events.related_entity_ids) WHERE json_each.value = ?)",
			userID, loserVCardUID)},
		{&c.ConversationAgendaItems, db.Model(&models.ConversationAgenda{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.GiftItems, db.Model(&models.Gift{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.FieldValues, db.Model(&models.FieldValue{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.ContactSyncLinks, db.Model(&models.ContactSyncLink{}).Where("contact_id = ? AND user_id = ?", loserID, userID)},
		{&c.Attachments, db.Model(&models.Attachment{}).Where("contact_vcard_uid = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.Preferences, db.Model(&models.Preference{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.ExternalIdentities, db.Model(&models.ExternalIdentity{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.ExternalActivities, db.Model(&models.ExternalActivity{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.CadencePolicies, db.Model(&models.CadencePolicy{}).Where("entity_id = ? AND user_id = ?", loserVCardUID, userID)},
		{&c.ReachOutSuggestions, db.Model(&models.ReachOutSuggestion{}).Where("contact_vcard_uid = ? AND user_id = ?", loserVCardUID, userID)},
	}
	for _, s := range steps {
		if err := s.q.Count(s.dst).Error; err != nil {
			return c, err
		}
	}
	if err := db.Raw(
		"SELECT COUNT(*) FROM activity_contacts ac JOIN activities a ON a.id = ac.activity_id "+
			"WHERE ac.contact_id = ? AND a.user_id = ?",
		loserID, userID,
	).Scan(&c.Activities).Error; err != nil {
		return c, err
	}
	return c, nil
}

// RepointContactAssociations moves every association off loser onto keeper,
// inside tx (must already be inside a db.Transaction). Does NOT touch
// ContactSyncLink and does NOT delete the loser row itself -- both stay the
// job of the shared deleteContactAssociations/tx.Delete(&loser) calls the
// controller makes right after this returns, so ContactSyncLink cleanup is
// never duplicated and can never drift from DeleteContact's own behavior.
// fvConflicts/resolutions apply the user's chosen value onto whichever
// FieldValue row survives a collision (see the FieldValue section below).
// Returns the number of RelationshipEdge rows dropped as a self-loop or
// semantic duplicate, for the audit note.
func RepointContactAssociations(
	tx *gorm.DB, userID uint, keeper, loser *models.Contact,
	fvConflicts []models.ContactMergeFieldConflict, resolutions map[string]string,
) (int, error) {
	// Notes / Reminders / ReminderCompletions: plain repoint, no unique
	// constraint on contact_id.
	if err := tx.Model(&models.Note{}).Where("contact_id = ? AND user_id = ?", loser.ID, userID).
		Update("contact_id", keeper.ID).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&models.Reminder{}).Where("contact_id = ? AND user_id = ?", loser.ID, userID).
		Update("contact_id", keeper.ID).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&models.ReminderCompletion{}).Where("contact_id = ? AND user_id = ?", loser.ID, userID).
		Update("contact_id", keeper.ID).Error; err != nil {
		return 0, err
	}

	// activity_contacts: composite PK (activity_id, contact_id), no Go
	// model. Dedup-delete the loser's row where the keeper is already on the
	// same activity (would otherwise violate the PK), then repoint the rest.
	// Raw SQL matches the one existing precedent for touching this table
	// (DeleteContact) -- no ON CONFLICT/INSERT OR IGNORE idiom exists
	// elsewhere in this codebase to prefer instead.
	if err := tx.Exec(
		"DELETE FROM activity_contacts WHERE contact_id = ? AND activity_id IN "+
			"(SELECT activity_id FROM activity_contacts WHERE contact_id = ?) "+
			"AND activity_id IN (SELECT id FROM activities WHERE user_id = ?)",
		loser.ID, keeper.ID, userID,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(
		"UPDATE activity_contacts SET contact_id = ? WHERE contact_id = ? "+
			"AND activity_id IN (SELECT id FROM activities WHERE user_id = ?)",
		keeper.ID, loser.ID, userID,
	).Error; err != nil {
		return 0, err
	}

	// household_members / circle_members / contact_tags: each has a
	// uniqueIndex on (container_id, member_vcard_uid) -- same dedup-then-
	// repoint shape as activity_contacts, one pass per table.
	if err := tx.Exec(
		"DELETE FROM household_members WHERE member_vcard_uid = ? AND user_id = ? AND household_id IN "+
			"(SELECT household_id FROM household_members WHERE member_vcard_uid = ? AND user_id = ?)",
		loser.VCardUID, userID, keeper.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(
		"UPDATE household_members SET member_vcard_uid = ? WHERE member_vcard_uid = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}

	if err := tx.Exec(
		"DELETE FROM circle_members WHERE member_vcard_uid = ? AND user_id = ? AND circle_id IN "+
			"(SELECT circle_id FROM circle_members WHERE member_vcard_uid = ? AND user_id = ?)",
		loser.VCardUID, userID, keeper.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(
		"UPDATE circle_members SET member_vcard_uid = ? WHERE member_vcard_uid = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}

	if err := tx.Exec(
		"DELETE FROM contact_tags WHERE contact_vcard_uid = ? AND user_id = ? AND tag_id IN "+
			"(SELECT tag_id FROM contact_tags WHERE contact_vcard_uid = ? AND user_id = ?)",
		loser.VCardUID, userID, keeper.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(
		"UPDATE contact_tags SET contact_vcard_uid = ? WHERE contact_vcard_uid = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}

	// attachments (N7) / preferences (T20a): plain repoint, neither has a
	// uniqueness constraint to dedupe against (T107).
	if err := tx.Model(&models.Attachment{}).Where("contact_vcard_uid = ? AND user_id = ?", loser.VCardUID, userID).
		Update("contact_vcard_uid", keeper.VCardUID).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&models.Preference{}).Where("entity_id = ? AND user_id = ?", loser.VCardUID, userID).
		Update("entity_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// reach_out_suggestions (issue #177): no unique constraint, but a plain
	// repoint could leave two pending suggestions of the same kind on the
	// keeper if both contacts already had one (e.g. both flagged an
	// "organization" change) -- creation-time dedup (pendingSuggestionExists)
	// never sees this cross-contact case, so dedupe here too, same
	// dedupe-then-repoint shape as household_members/circle_members/
	// contact_tags above: drop the loser's pending row where the keeper
	// already has a pending suggestion of the same kind, then repoint the
	// rest.
	if err := tx.Exec(
		"DELETE FROM reach_out_suggestions WHERE contact_vcard_uid = ? AND user_id = ? AND status = ? AND kind IN "+
			"(SELECT kind FROM reach_out_suggestions WHERE contact_vcard_uid = ? AND user_id = ? AND status = ?)",
		loser.VCardUID, userID, models.ReachOutStatusPending, keeper.VCardUID, userID, models.ReachOutStatusPending,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&models.ReachOutSuggestion{}).Where("contact_vcard_uid = ? AND user_id = ?", loser.VCardUID, userID).
		Update("contact_vcard_uid", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// external_identities / external_activities (T14): the natural-key
	// unique index is (system, external_id, user_id) / (source_system,
	// external_id, user_id) -- entity_id is NOT part of it, which means it
	// unique-constrains across the whole user, not per-contact. So the
	// collision a dedupe step would guard against (keeper and loser each
	// already linking the same external record) cannot exist to begin with:
	// the constraint already forbids two rows sharing that key for this user
	// regardless of which contact holds them, before a merge ever starts.
	// Re-pointing entity_id doesn't touch the key, so a plain repoint (T107)
	// can never violate it -- confirmed by a real-DB test attempting to seed
	// that exact "shared" fixture, which fails at Create, not at merge.
	if err := tx.Exec(
		"UPDATE external_identities SET entity_id = ? WHERE entity_id = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}
	if err := tx.Exec(
		"UPDATE external_activities SET entity_id = ? WHERE entity_id = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}

	// field_values: uniqueIndex on (field_definition_id, entity_id). Where
	// both sides had a value (fvConflicts), drop the loser's row and apply
	// the user's chosen value onto the keeper's surviving row -- a value
	// genuinely cannot be unioned, unlike every other association. Where
	// only one side had a value, plain repoint.
	if err := tx.Exec(
		"DELETE FROM field_values WHERE entity_id = ? AND user_id = ? AND field_definition_id IN "+
			"(SELECT field_definition_id FROM field_values WHERE entity_id = ? AND user_id = ?)",
		loser.VCardUID, userID, keeper.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}
	for _, conflict := range fvConflicts {
		chosen, ok := resolutions[conflict.Field]
		if !ok {
			return 0, fmt.Errorf("contact merge: unresolved field value conflict for %q", conflict.Field)
		}
		if err := tx.Exec(
			"UPDATE field_values SET value = ? WHERE entity_id = ? AND user_id = ? AND field_definition_id = ?",
			chosen, keeper.VCardUID, userID, conflict.Field,
		).Error; err != nil {
			return 0, err
		}
	}
	if err := tx.Exec(
		"UPDATE field_values SET entity_id = ? WHERE entity_id = ? AND user_id = ?",
		keeper.VCardUID, loser.VCardUID, userID,
	).Error; err != nil {
		return 0, err
	}

	// LifeEvent: two independent sub-parts. (1) the loser's own subject
	// (EntityID) -- plain bulk UPDATE, no unique constraint. Run first so
	// part (2)'s self-reference check below also catches rows whose
	// EntityID was just re-pointed in this same transaction.
	if err := tx.Model(&models.LifeEvent{}).Where("entity_id = ? AND user_id = ?", loser.VCardUID, userID).
		Update("entity_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// ConversationAgenda items are keyed by the same EntityID convention
	// (Contact.VCardUID) as LifeEvents, with no unique constraint -- a
	// plain bulk repoint, so agenda items follow the surviving contact.
	if err := tx.Model(&models.ConversationAgenda{}).Where("entity_id = ? AND user_id = ?", loser.VCardUID, userID).
		Update("entity_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// Gift records are keyed by the same EntityID convention, with no unique
	// constraint -- a plain bulk repoint, so gifts follow the surviving
	// contact. Any linked LifeEventID reference keeps pointing at the same
	// LifeEvent row, which itself has been (or is being) re-pointed to the
	// keeper just above, so the link stays consistent.
	if err := tx.Model(&models.Gift{}).Where("entity_id = ? AND user_id = ?", loser.VCardUID, userID).
		Update("entity_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// (2) RelatedEntityIDs -- a JSON array serialized into a TEXT column
	// with no per-element SQL write path. json_each cheaply finds candidate
	// rows; the rewrite itself happens in Go.
	var candidates []models.LifeEvent
	if err := tx.Where(
		"user_id = ? AND related_entity_ids IS NOT NULL AND EXISTS "+
			"(SELECT 1 FROM json_each(life_events.related_entity_ids) WHERE json_each.value = ?)",
		userID, loser.VCardUID,
	).Find(&candidates).Error; err != nil {
		return 0, err
	}
	for i := range candidates {
		ev := &candidates[i]
		changed := false
		for j, id := range ev.RelatedEntityIDs {
			if id == loser.VCardUID {
				ev.RelatedEntityIDs[j] = keeper.VCardUID
				changed = true
			}
		}
		if !changed {
			continue
		}
		ev.RelatedEntityIDs = dedupStrings(ev.RelatedEntityIDs)
		if ev.EntityID == keeper.VCardUID {
			ev.RelatedEntityIDs = removeString(ev.RelatedEntityIDs, keeper.VCardUID)
		}
		if err := tx.Save(ev).Error; err != nil {
			return 0, err
		}
	}

	// cadence_policies (T19): one-per-contact (migration 000002's partial
	// unique index on (user_id, entity_id)), so unlike everything above it
	// can genuinely conflict rather than just dedupe. See
	// repointCadencePolicy and ComputeCadencePolicyConflict (T107).
	if err := repointCadencePolicy(tx, userID, keeper, loser, resolutions); err != nil {
		return 0, err
	}

	// users.self_contact_vcard_uid (T90): the user's "Me" pointer. One row
	// per user, no uniqueness hazard — if it pointed at the loser (the loser
	// was "Me"), move it to the keeper so it doesn't dangle once the loser is
	// soft-deleted below. If it pointed at the keeper or is NULL, the WHERE
	// matches nothing and the write is a no-op.
	if err := tx.Model(&models.User{}).Where("id = ? AND self_contact_vcard_uid = ?", userID, loser.VCardUID).
		Update("self_contact_vcard_uid", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	// RelationshipEdge: repoint both endpoints onto the keeper, dropping
	// self-loops and semantic duplicates. relationship_edges carries a
	// natural-key unique index (migration 000055, issue #928), so the old
	// bulk UPDATE-then-dedup order would transiently collide with an existing
	// keeper edge and abort the merge; the surviving set is recomputed in Go
	// first (repointRelationshipEdges).
	edgesDropped, err := repointRelationshipEdges(tx, userID, keeper, loser)
	if err != nil {
		return 0, err
	}

	return edgesDropped, nil
}

// cadencePolicyConflictField is the fixed conflict key for a CadencePolicy
// collision, in the same key space as mergeScalarFields' keys (T107). It's
// appended into ContactMergeResolution.Conflicts rather than getting its own
// response field, which is what lets MergeContactsDialog.tsx's existing
// generic radio-choice UI for scalar conflicts handle it with zero frontend
// changes.
const cadencePolicyConflictField = "cadence_policy"

// ComputeCadencePolicyConflict returns a conflict when both keeper and loser
// already have a CadencePolicy (T19) and they genuinely differ -- migration
// 000002's partial unique index on (user_id, entity_id) means only one can
// survive per contact, so unlike every other T107 addition this can't just be
// unioned or plain-repointed. nil, nil when at most one side has a policy
// (repointCadencePolicy adopts a one-sided policy silently) or when both
// sides already agree (nothing to ask).
func ComputeCadencePolicyConflict(db *gorm.DB, userID uint, keeperVCardUID, loserVCardUID string) (*models.ContactMergeFieldConflict, error) {
	var keeperPolicy, loserPolicy models.CadencePolicy
	if err := db.Where("entity_id = ? AND user_id = ?", keeperVCardUID, userID).First(&keeperPolicy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if err := db.Where("entity_id = ? AND user_id = ?", loserVCardUID, userID).First(&loserPolicy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	keeperSummary, loserSummary := formatCadencePolicySummary(keeperPolicy), formatCadencePolicySummary(loserPolicy)
	if keeperSummary == loserSummary {
		return nil, nil
	}
	return &models.ContactMergeFieldConflict{
		Field:       cadencePolicyConflictField,
		Label:       "Stay-in-touch cadence",
		KeeperValue: keeperSummary,
		LoserValue:  loserSummary,
	}, nil
}

// formatCadencePolicySummary renders a CadencePolicy identically everywhere
// it's compared or displayed -- ComputeCadencePolicyConflict's KeeperValue/
// LoserValue and repointCadencePolicy's re-derivation of which side the
// caller picked -- so the two can never drift apart (the same trick
// BuildContactMergeNoteContent already relies on for scalar conflicts).
// QualifyingTypes is sorted first: it's a JSON array with no canonical
// order, so two policies that are the same set of types in a different
// insertion order must still compare equal, not surface as a spurious
// conflict.
func formatCadencePolicySummary(p models.CadencePolicy) string {
	if len(p.QualifyingTypes) == 0 {
		return fmt.Sprintf("Every %d days", p.TargetIntervalDays)
	}
	sorted := append([]string(nil), p.QualifyingTypes...)
	sort.Strings(sorted)
	return fmt.Sprintf("Every %d days (%s)", p.TargetIntervalDays, strings.Join(sorted, ", "))
}

// repointCadencePolicy resolves CadencePolicy across a merge, inside tx:
// nothing on the loser -> no-op; only the loser has one -> repoint it
// silently; both have one -> apply whichever ComputeCadencePolicyConflict's
// caller resolved (or, if the two already agree, keep the keeper's and drop
// the loser's with no resolution required, mirroring how identical scalar
// values never become a conflict in the first place).
func repointCadencePolicy(tx *gorm.DB, userID uint, keeper, loser *models.Contact, resolutions map[string]string) error {
	var keeperPolicy models.CadencePolicy
	keeperErr := tx.Where("entity_id = ? AND user_id = ?", keeper.VCardUID, userID).First(&keeperPolicy).Error
	if keeperErr != nil && !errors.Is(keeperErr, gorm.ErrRecordNotFound) {
		return keeperErr
	}

	var loserPolicy models.CadencePolicy
	loserErr := tx.Where("entity_id = ? AND user_id = ?", loser.VCardUID, userID).First(&loserPolicy).Error
	if errors.Is(loserErr, gorm.ErrRecordNotFound) {
		return nil // nothing to move
	} else if loserErr != nil {
		return loserErr
	}

	if errors.Is(keeperErr, gorm.ErrRecordNotFound) {
		// Only the loser has one -- adopt silently.
		return tx.Model(&models.CadencePolicy{}).Where("id = ?", loserPolicy.ID).
			Update("entity_id", keeper.VCardUID).Error
	}

	keeperSummary, loserSummary := formatCadencePolicySummary(keeperPolicy), formatCadencePolicySummary(loserPolicy)
	if keeperSummary != loserSummary {
		chosen, ok := resolutions[cadencePolicyConflictField]
		if !ok {
			return fmt.Errorf("contact merge: unresolved cadence policy conflict")
		}
		switch chosen {
		case loserSummary:
			// The loser's policy wins: drop the keeper's, then repoint the
			// loser's onto the keeper.
			if err := tx.Delete(&keeperPolicy).Error; err != nil {
				return err
			}
			return tx.Model(&models.CadencePolicy{}).Where("id = ?", loserPolicy.ID).
				Update("entity_id", keeper.VCardUID).Error
		case keeperSummary:
			// Explicitly the keeper's: fall through to the drop-the-loser's
			// return below.
		default:
			// Doesn't match either side -- e.g. one policy was edited
			// between preview and commit, so `chosen` (echoed back from a
			// stale preview) no longer matches this freshly-recomputed
			// summary. Reject rather than silently guessing which policy
			// the user actually meant (unlike a scalar field conflict,
			// picking wrong here doesn't just write an odd string -- it
			// decides which whole row survives).
			return fmt.Errorf("contact merge: cadence policy resolution %q does not match either side", chosen)
		}
	}
	// Keeper's policy wins (explicitly chosen, or the two already agreed):
	// drop the loser's, the keeper's is untouched.
	return tx.Delete(&loserPolicy).Error
}

// repointRelationshipEdges moves the loser's relationship edges onto the keeper
// and returns how many were dropped as self-loops or semantic duplicates.
//
// relationship_edges carries a natural-key unique index (migration 000055,
// issue #928) on (user_id, source_id, target_id, type), so a plain bulk
// repoint that lands a loser edge on a tuple the keeper already holds would
// fail mid-UPDATE and abort the merge. The whole surviving set is therefore
// recomputed in Go first — applying the same edgesConflict/pickEdgeToDrop
// authority rules the pre-index code ran after the repoint — then the dropped
// rows on both sides are deleted and the survivors rewritten. The merge
// outcome is unchanged: the more authoritative duplicate survives regardless
// of which contact it was attached to.
func repointRelationshipEdges(tx *gorm.DB, userID uint, keeper, loser *models.Contact) (int, error) {
	var edges []models.RelationshipEdge
	if err := tx.Where(
		"user_id = ? AND (source_id IN ? OR target_id IN ?)",
		userID, []string{keeper.VCardUID, loser.VCardUID}, []string{keeper.VCardUID, loser.VCardUID},
	).Find(&edges).Error; err != nil {
		return 0, err
	}

	repointed := make([]models.RelationshipEdge, 0, len(edges))
	toDelete := map[string]bool{}
	for _, e := range edges {
		if e.SourceID == loser.VCardUID {
			e.SourceID = keeper.VCardUID
		}
		if e.TargetID == loser.VCardUID {
			e.TargetID = keeper.VCardUID
		}
		// A self-loop on the keeper — whether pre-existing or produced by the
		// repoint (the merge of two contacts that were related to each other)
		// — is never a valid edge.
		if e.SourceID == e.TargetID {
			toDelete[e.ID] = true
			continue
		}
		repointed = append(repointed, e)
	}

	// Mark every edge that conflicts with a more authoritative one.
	for i := 0; i < len(repointed); i++ {
		if toDelete[repointed[i].ID] {
			continue
		}
		for j := i + 1; j < len(repointed); j++ {
			if toDelete[repointed[j].ID] || !edgesConflict(repointed[i], repointed[j]) {
				continue
			}
			toDelete[pickEdgeToDrop(repointed[i], repointed[j]).ID] = true
		}
	}

	if len(toDelete) > 0 {
		ids := make([]string, 0, len(toDelete))
		for id := range toDelete {
			ids = append(ids, id)
		}
		if err := tx.Where("id IN ?", ids).Delete(&models.RelationshipEdge{}).Error; err != nil {
			return 0, err
		}
	}

	// Delete first, then repoint in two bulk writes. Every self-loop and
	// semantic duplicate was removed above, so no surviving edge can land on
	// an occupied natural key.
	if err := tx.Model(&models.RelationshipEdge{}).Where("user_id = ? AND source_id = ?", userID, loser.VCardUID).
		Update("source_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&models.RelationshipEdge{}).Where("user_id = ? AND target_id = ?", userID, loser.VCardUID).
		Update("target_id", keeper.VCardUID).Error; err != nil {
		return 0, err
	}

	return len(toDelete), nil
}

// edgesConflict reports whether a and b express the same relationship fact
// -- literally identical, or mutual inverses (A rel_of B and
// B InverseRelationType(rel)_of A).
func edgesConflict(a, b models.RelationshipEdge) bool {
	exact := a.SourceID == b.SourceID && a.TargetID == b.TargetID && a.Type == b.Type
	inv := models.InverseRelationType(a.Type)
	inverse := inv != "" && a.SourceID == b.TargetID && a.TargetID == b.SourceID && b.Type == inv
	return exact || inverse
}

// pickEdgeToDrop picks which of two conflicting edges to remove, keeping the
// more authoritative one: higher Confidence wins; ties broken by Status
// (confirmed beats suggested -- confirmed edges are always Confidence 1.0
// per RelationshipEdge.Source's doc comment, so this mostly agrees with the
// Confidence check and only matters on an exact tie); remaining ties broken
// by earlier CreatedAt (the longer-standing record survives); final
// tiebreak is the lexicographically smaller ID (edge IDs are random UUIDs,
// not meaningfully ordered, but this guarantees a deterministic result with
// no other signal left to break on).
func pickEdgeToDrop(a, b models.RelationshipEdge) models.RelationshipEdge {
	keep := a
	switch {
	case a.Confidence != b.Confidence:
		if b.Confidence > a.Confidence {
			keep = b
		}
	case a.Status != b.Status:
		if b.Status == models.RelationshipStatusConfirmed {
			keep = b
		}
	case !a.CreatedAt.Equal(b.CreatedAt):
		if b.CreatedAt.Before(a.CreatedAt) {
			keep = b
		}
	default:
		if b.ID < a.ID {
			keep = b
		}
	}
	if keep.ID == a.ID {
		return b
	}
	return a
}

// dedupStrings removes duplicate values from s, preserving first-occurrence order.
func dedupStrings(s []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(s))
	for _, v := range s {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// removeString returns s with every occurrence of v removed.
func removeString(s []string, v string) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// BuildContactMergeNoteContent composes the audit note written to the
// keeper, recording what a merge changed. Not built via import_service.go's
// CreateMergeNote -- that function's newValues map[string]interface{}
// parameter only diffs a fixed scalar set (plus "circles" specially) and has
// no seam for arrays/associations/FieldValue conflicts; force-fitting it
// would fight its shape more than it would save. CreateMergeNote itself is
// untouched, and its own pinned tests (import_service_merge_test.go) stay
// green.
func BuildContactMergeNoteContent(
	loser *models.Contact, res *models.ContactMergeResolution, resolutions map[string]string,
	counts models.ContactMergeAssociationCounts, edgesDropped int,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Merged contact #%d (%s) into this record.\n", loser.ID, strings.TrimSpace(loser.Firstname+" "+loser.Lastname))

	if len(res.Conflicts) > 0 {
		b.WriteString("\nResolved conflicts:\n")
		for _, conflict := range res.Conflicts {
			chosen := resolutions[conflict.Field]
			switch chosen {
			case conflict.KeeperValue:
				fmt.Fprintf(&b, "- %s: kept %q (merged contact had %q)\n", conflict.Label, chosen, conflict.LoserValue)
			case conflict.LoserValue:
				fmt.Fprintf(&b, "- %s: took %q from the merged contact (was %q)\n", conflict.Label, chosen, conflict.KeeperValue)
			default:
				fmt.Fprintf(&b, "- %s: set to %q (was %q, merged contact had %q)\n", conflict.Label, chosen, conflict.KeeperValue, conflict.LoserValue)
			}
		}
	}
	if len(res.FieldValueConflicts) > 0 {
		b.WriteString("\nResolved custom field conflicts:\n")
		for _, conflict := range res.FieldValueConflicts {
			chosen := resolutions[conflict.Field]
			fmt.Fprintf(&b, "- %s: set to %q (was %q, merged contact had %q)\n", conflict.Label, chosen, conflict.KeeperValue, conflict.LoserValue)
		}
	}

	b.WriteString("\nRe-pointed: ")
	fmt.Fprintf(&b, "%d notes, %d activities, %d reminders, %d reminder completions, "+
		"%d relationship edges (%d dropped as duplicate/self-loop), %d household memberships, "+
		"%d circle memberships, %d tags, %d life events (%d references), %d custom field values, "+
		"%d attachments, %d preferences, %d external identities, %d external activities, "+
		"%d cadence policies.\n",
		counts.Notes, counts.Activities, counts.Reminders, counts.ReminderCompletions,
		counts.RelationshipEdges, edgesDropped, counts.HouseholdMemberships,
		counts.CircleMemberships, counts.Tags, counts.LifeEvents, counts.LifeEventReferences, counts.FieldValues,
		counts.Attachments, counts.Preferences, counts.ExternalIdentities, counts.ExternalActivities,
		counts.CadencePolicies)

	if counts.ContactSyncLinks > 0 {
		fmt.Fprintf(&b, "%d CardDAV sync link(s) on the merged contact were discarded (not re-pointed).\n", counts.ContactSyncLinks)
	}
	return b.String()
}
