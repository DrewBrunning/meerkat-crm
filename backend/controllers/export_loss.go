package controllers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"mycorrhizal/contactmodel"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/jscontact"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"mycorrhizal/vcard3"
	"mycorrhizal/vcard4"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// DATA-02 (issue #442): export-loss reporting. The adapters already produce a
// contactmodel.Diagnostic per fidelity-loss event (a field with no home in the
// target format, a lossy degradation); these entry points surface those
// diagnostics to the user — a preflight endpoint that computes what an export
// would lose without producing the file, and a response header on the actual
// export carrying what it did lose.
//
// Both consume one shared code path (renderContactExport below): the preflight
// discards the file bytes, the export handlers send them. The two can differ
// if data changes between the calls — which is itself worth knowing.

// Export format tokens. These are the wire values for the preflight endpoint's
// ?format= param and must match correspondence.Format string values so the
// DATA-01 matrix classification resolves.
const (
	exportFormatVCard4    = "vcard4"
	exportFormatVCard3    = "vcard3"
	exportFormatJSContact = "jscontact"
)

// validExportFormats is the closed set of format tokens the preflight endpoint
// accepts.
var validExportFormats = map[string]bool{
	exportFormatVCard4:    true,
	exportFormatVCard3:    true,
	exportFormatJSContact: true,
}

// adapterForFormat returns the neutral-model exporter for a format token.
// The token is validated by the caller; an unknown token here is a
// programming error, not a request error.
func adapterForFormat(format string) contactmodel.Exporter {
	switch format {
	case exportFormatVCard4:
		return vcard4.Adapter{}
	case exportFormatVCard3:
		return vcard3.Adapter{}
	case exportFormatJSContact:
		return jscontact.Adapter{}
	}
	return nil
}

// renderContactExport runs one contact through one format adapter and returns
// the file bytes plus every loss Diagnostic the format produced (envelope-loss
// diagnostics + adapter diagnostics). This is the single shared computation
// behind the export handlers and the preflight endpoint (issue #442): the
// preflight discards the bytes, the export handlers write them.
func renderContactExport(contact *models.Contact, photoDir, format string, db *gorm.DB, sel *models.FieldSelection, log *zerolog.Logger) ([]byte, []contactmodel.Diagnostic, error) {
	record := models.RecordForContactFiltered(contact, photoDir, db, sel)
	diags := models.EnvelopeExportLossDiagnostics(record)
	data, adapterDiags, err := adapterForFormat(format).Export(record)
	diags = append(diags, adapterDiags...)
	for _, d := range diags {
		log.Debug().Str("severity", d.Severity).Str("concept", d.Concept).Uint("contact_id", contact.ID).Msg(logger.SanitizeLogField(d.Message))
	}
	return data, diags, err
}

// contactLossReports converts one contact's raw diagnostics into the
// user-facing loss reports for the format. Wrapper over models.LossReportsFor
// so the controllers read the same way the preflight response and the export
// header are built.
func contactLossReports(format string, contact *models.Contact, diags []contactmodel.Diagnostic) []models.LossReport {
	return models.LossReportsFor(format, contact, diags)
}

// exportLossReportHeader is the response header carrying a file-download
// export's loss report (issue #442). The value is a URL-encoded JSON object
// (see openapi.yaml): {"format", "count", "truncated", "diagnostics"}.
//
// The diagnostics list is bounded to maxExportLossHeaderBytes (below): a file
// download's header must stay within proxy limits, so beyond that bound the
// header carries the total count, the diagnostics that fit, and
// truncated=true. The complete list for the same parameters is always
// available via GET /export/preflight.
const exportLossReportHeader = "X-Mycorrhizal-Export-Loss-Report"

// maxExportLossHeaderBytes bounds the URL-encoded loss-report JSON carried in
// exportLossReportHeader.
//
// Issue #863: the previous 6 KB bound assumed "typical proxy header limits
// (~8KB)". That is the wrong model. nginx — the proxy in this project's own
// shipped all-in-one image — buffers the *entire* upstream response header
// block into a single proxy_buffer_size buffer, whose compiled default is one
// memory page (4 KB on the linux/amd64 Alpine build we ship). A 6 KB value in
// this one header alone overran that buffer, so nginx returned
// "502 Bad Gateway" ("upstream sent too big header") in place of the export —
// while the CSV export, which sets no such header, kept working. The shipped
// nginx.conf now also raises proxy_buffer_size on /api/ (defense in depth),
// but the header must be small enough to pass a *stock*-configured reverse
// proxy too: 3 KB leaves ~1 KB of a 4 KB buffer for the status line and the
// dozen other response headers. Truncation past this bound is unchanged —
// truncated=true, the true total in count, the full list via GET
// /export/preflight.
const maxExportLossHeaderBytes = 3072

// exportLossHeader is the JSON object carried in exportLossReportHeader.
// Diagnostics is never omitted so the client can distinguish an empty report
// (an absent diagnostics key would be a parse error, not "no losses").
type exportLossHeader struct {
	Format      string              `json:"format"`
	Count       int                 `json:"count"`
	Truncated   bool                `json:"truncated"`
	Diagnostics []models.LossReport `json:"diagnostics"`
}

// setExportLossReportHeader writes the loss-report header for an export
// response, bounding the diagnostics to maxExportLossHeaderBytes with a
// truncated flag when the full list does not fit.
func setExportLossReportHeader(c *gin.Context, format string, reports []models.LossReport) {
	header := exportLossHeader{Format: format, Count: len(reports), Diagnostics: reports}
	payload := header
	for {
		raw, err := json.Marshal(payload)
		if err != nil { // # pragma: no cover — LossReport has no unmarshalable field; a marshal failure here is impossible
			return
		}
		encoded := url.QueryEscape(string(raw))
		if len(encoded) <= maxExportLossHeaderBytes || len(payload.Diagnostics) == 0 {
			c.Header(exportLossReportHeader, encoded)
			return
		}
		// Drop diagnostics from the tail until the header fits; the count
		// always reflects the true total and truncated flags the gap.
		payload.Truncated = true
		payload.Diagnostics = payload.Diagnostics[:len(payload.Diagnostics)-1]
	}
}

// ExportPreflight computes what an export would lose for the requested
// format/selection/scope without producing the file (issue #442). It runs the
// exact same per-contact computation the export handlers run — the shared
// renderContactExport — so preflight and the actual export agree for unchanged
// data; the response carries the loss reports only.
//
// Request shape mirrors the export handlers: ?format=vcard4|vcard3|jscontact
// (required), plus the same sections/include_sensitive/vcard_uid params.
func ExportPreflight(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	log := logger.FromContext(c)
	photoDir := currentConfig(c).ProfilePhotoDir

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	format := strings.TrimSpace(c.Query("format"))
	if !validExportFormats[format] {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("format",
			"format must be one of vcard4, vcard3, jscontact").WithDetails("operation", exportOpPreflight).WithDetails("category", exportCatValidation))
		return
	}

	sel, ok := parseExportFieldSelection(c, exportOpPreflight)
	if !ok {
		return
	}

	// Same contact scope as the export handlers (user-scoped, optionally
	// narrowed to one VCardUID).
	query := db.Where("user_id = ?", userID)
	if vcardUID := strings.TrimSpace(c.Query("vcard_uid")); vcardUID != "" {
		query = query.Where("vcard_uid = ?", vcardUID)
	}
	var contacts []models.Contact
	if err := query.
		Order("firstname ASC, lastname ASC").
		Find(&contacts).Error; err != nil {
		abortExportError(c, log, exportOpPreflight, exportCatDatabase, "Failed to fetch contacts for export preflight", err)
		return
	}

	reports := make([]models.LossReport, 0)
	for i := range contacts {
		_, diags, err := renderContactExport(&contacts[i], photoDir, format, db, sel, log)
		if err != nil { // # pragma: no cover — defensive: an adapter error is reserved for malformed format instances (contactmodel.Exporter contract) and cannot fire for a stored, valid contact
			// Same tolerance as the export handlers: a per-contact encode
			// failure logs and moves on instead of failing the whole request.
			log.Error().Err(err).Uint("contact_id", contacts[i].ID).Msg("Failed to encode contact during export preflight")
			continue
		}
		reports = append(reports, contactLossReports(format, &contacts[i], diags)...)
	}

	c.JSON(http.StatusOK, models.ExportLossPreflightResponse{
		Format:       format,
		ContactCount: len(contacts),
		Diagnostics:  reports,
	})
}
