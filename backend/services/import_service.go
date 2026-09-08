package services

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mycorrhizal/contactmodel"
	"mycorrhizal/jscontact"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/photostore"
	"mycorrhizal/vcard3"
	"mycorrhizal/vcard4"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// Import limits
//
// Sized for T56:
// a Google Takeout or other contacts-app full export can run into the
// hundreds of contacts — and, for VCF, carry a photo per contact — so these
// caps are generous enough to bring an entire existing address book in one
// pass, not just a handful at a time. Memory stays bounded because the row
// caps and the byte-size caps interact: a VCF file can't reach 20000 contacts
// within the 50MB size cap unless those contacts are tiny.
const (
	MaxCSVSize     = 20 * 1024 * 1024 // 20MB
	MaxVCFSize     = 50 * 1024 * 1024 // 50MB (VCF files can include embedded photos)
	MaxCSVRows     = 20000
	MaxVCFContacts = 20000
	SampleRows     = 3 // Number of sample rows to return
)

// VCFContactData holds parsed VCF contact data with photo for import
type VCFContactData struct {
	Contact        *models.Contact
	PhotoData      []byte
	PhotoMediaType string
	PhotoURL       string // URL to fetch photo from (if not embedded)
}

// ParseCSV reads and parses a CSV file, returning headers and data rows
func ParseCSV(reader io.Reader) (headers []string, rows [][]string, err error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1 // Allow variable number of fields
	csvReader.LazyQuotes = true    // Be lenient with quotes

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CSV format: %w", err)
	}

	if len(records) == 0 {
		return nil, nil, fmt.Errorf("CSV file is empty")
	}

	headers = records[0]
	if len(headers) == 0 {
		return nil, nil, fmt.Errorf("CSV file has no headers")
	}

	rows = records[1:]
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("CSV file has no data rows")
	}

	if len(rows) > MaxCSVRows {
		return nil, nil, fmt.Errorf("too many rows: maximum is %d rows", MaxCSVRows)
	}

	return headers, rows, nil
}

// splitVCardBlocks splits raw multi-contact VCF bytes into individual
// per-card blocks (each including its own BEGIN:VCARD/END:VCARD framing).
// It is a line-based walker rather than the old
// `BEGIN:VCARD.*?END:VCARD` regex for two adversarial-input reasons
// (TEST-04, issue #432), both real silent data-loss bugs the regex shipped:
//
//   - A property *value* can contain the literal text "END:VCARD" or
//     "BEGIN:VCARD" on the same physical line (real notes do this). An
//     unanchored `.*?END:VCARD` split truncates the value at the first
//     in-value occurrence. Only a line that IS the delimiter (case
//     -insensitive, per RFC 6350/2426) is treated as a boundary.
//   - A truncated card (missing END) must not lazily swallow the next
//     card's lines up to the next END, merging two cards into one
//     malformed block. The walker closes a card at a second BEGIN:VCARD,
//     so the truncated card is emitted on its own — it then fails to
//     parse and is *reported* as a per-row error instead of silently
//     deleting its neighbor (the multi-malformed-middle fixture's bound).
//
// Interstitial garbage between cards is dropped, as the regex did; naming
// that garbage for the user is DATA-02's reporting job (issue #442), not
// the splitter's.
func splitVCardBlocks(data []byte) [][]byte {
	lines := strings.Split(string(normalizeVCardLineEndings(data)), "\n")

	var blocks [][]byte
	var current []string
	inCard := false
	flush := func() {
		if inCard {
			blocks = append(blocks, []byte(strings.Join(current, "\r\n")+"\r\n"))
			current = nil
			inCard = false
		}
	}
	for _, line := range lines {
		// Trailing whitespace only — leading whitespace marks a folded
		// continuation line, which must never be read as a delimiter.
		token := strings.ToUpper(strings.TrimRight(line, " \t"))
		if token == "BEGIN:VCARD" {
			flush() // a second BEGIN before an END: previous card was truncated
			current = []string{line}
			inCard = true
			continue
		}
		if inCard && token == "END:VCARD" {
			current = append(current, line)
			flush()
			continue
		}
		if inCard {
			current = append(current, line)
		}
	}
	flush()
	return blocks
}

// utf8BOM is the UTF-8 byte order mark some Windows exporters prepend to a
// .vcf/.json upload. Stripped from the raw bytes before block splitting /
// JSON decoding: neither go-vcard (which fails "no BEGIN field found") nor
// encoding/json accepts it, and it carries no information — refusing the
// whole file over three ignorable bytes would violate ADR-0002's
// "preserve, don't reject" policy for a trivially-removable encoding
// artifact (TEST-04 str-bom fixture). The adapters strip it too, so direct
// consumers (CardDAV reconcile) are equally tolerant.
var utf8BOM = []byte("\xef\xbb\xbf")

// stripUTF8BOM removes a single leading UTF-8 BOM, if present.
func stripUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

// normalizeVCardLineEndings converts CRLF and CR-only line endings to LF so
// downstream line-oriented parsing (the splitter, go-vcard, vCard 2.1
// reconstruction) never has to special-case a legacy exporter's choice.
func normalizeVCardLineEndings(data []byte) []byte {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return []byte(s)
}

// vcardVersionRE sniffs the VERSION property within one block, per this
// WP's explicit ask ("sniffing VERSION (4.0 vs 3.0)").
var vcardVersionRE = regexp.MustCompile(`(?im)^VERSION:\s*([0-9.]+)\s*$`)

// sniffVCardVersion returns "2.1" if the block's VERSION property starts
// with "2", "3.0" if it starts with "3", otherwise "4.0" (the default for a
// missing/unrecognized VERSION, matching this WP's "advertise 4.0 by
// default" precedent). vCard 2.1 (T50) is detected explicitly rather than
// falling through to the 4.0 default: it uses a legacy bare-token parameter
// grammar go-vcard's decoder cannot parse at all, the largest version gap
// this app has an adapter for, so it needs its own normalization pass (see
// normalizeVCard21) before either adapter can see it correctly.
func sniffVCardVersion(block []byte) string {
	m := vcardVersionRE.FindSubmatch(block)
	if m == nil {
		return "4.0"
	}
	switch v := strings.TrimSpace(string(m[1])); {
	case strings.HasPrefix(v, "2"):
		return "2.1"
	case strings.HasPrefix(v, "3"):
		return "3.0"
	default:
		return "4.0"
	}
}

// extractPhotoFromRecord extracts binary/URL photo data from the first
// Card.Media entry with Kind=="photo", to preserve the existing VCF-import
// photo-processing UX (ConfirmVCF in import_session.go) now that parsing
// goes through the vcard4/vcard3/jscontact adapters instead of
// carddav.VCardToContact. Delegates the actual decoding to
// photostore.DecodePhotoURI (the same helper backend/models uses to bridge
// Card.Media <-> Contact.Photo/PhotoThumbnail,  photo-bridging
// prerequisite) so there is one decode implementation, not two.
func extractPhotoFromRecord(rec *contactmodel.Record) (data []byte, mediaType string, url string) {
	if rec == nil {
		return nil, "", ""
	}
	var photo *contactmodel.Resource
	for i := range rec.Card.Media {
		if rec.Card.Media[i].Kind == "photo" {
			photo = &rec.Card.Media[i]
			break
		}
	}
	if photo == nil || photo.URI == "" {
		return nil, "", ""
	}
	return photostore.DecodePhotoURI(photo.URI, photo.MediaType)
}

// ImportVCardBlock parses a single BEGIN..END vCard block into the neutral
// model, applying the same VERSION sniffing + vCard 2.1 normalization +
// adapter dispatch that ParseVCF applies to every block of a multi-card
// file. It is ParseVCF's per-block routing extracted so single-card
// consumers (the adversarial corpus tier harness, internal/adversarial)
// exercise exactly the production routing instead of re-implementing it.
func ImportVCardBlock(block []byte) (*contactmodel.Record, []contactmodel.Diagnostic, error) {
	// Normalize line endings up front so a CR-only block (legacy Outlook
	// exports, TEST-04 ven-cr-only) parses identically to a CRLF one:
	// go-vcard's decoder splits physical lines on '\n' only, so raw CR-only
	// bytes would arrive as one giant line and fail "invalid BEGIN value".
	// ParseVCF's splitter already normalizes before it reaches this point,
	// so this is idempotent there and load-bearing for direct callers
	// (CardDAV reconcile, the adversarial harness).
	block = normalizeVCardLineEndings(block)
	var adapter contactmodel.Importer = vcard4.Adapter{}
	var normDiags []contactmodel.Diagnostic
	switch sniffVCardVersion(block) {
	case "2.1":
		// vCard 2.1's legacy bare-token parameter grammar and
		// QUOTED-PRINTABLE encoding are both opaque to go-vcard's
		// decoder (T50) -- normalize the raw bytes first. vcard3, not
		// vcard4, is the target adapter: its importMediaURI already
		// tolerates 2.1-style ENCODING=BASE64 (vcard4 only understands
		// native data: URIs), and 2.1's TYPE-token grammar is a subset
		// of 3.0's, not 4.0's.
		block, normDiags = normalizeVCard21(block)
		adapter = vcard3.Adapter{}
	case "3.0":
		adapter = vcard3.Adapter{}
	}

	record, diags, importErr := adapter.Import(block)
	diags = append(normDiags, diags...)
	return record, diags, importErr
}

// ParseVCF reads and parses a VCF file, returning contact data and previews.
//
// Per docs/adrs/0001-neutral-hub-and-spoke-contact-model.md, this now
// splits the file into per-card blocks, sniffs each block's VERSION, and
// routes it through the vcard4/vcard3 adapter accordingly — replacing the
// legacy carddav.VCardToContact mapper — then turns the resulting
// contactmodel.Record into a candidate *models.Contact via
// models.ApplyRecordToContact (shared Record->Contact mapping).
// DetectDuplicate/MergeImportedContact/CreateMergeNote/ContactToPreviewMap/
// ValidateImportedContact are all unchanged: they already operate on the
// resulting flat Contact fields, which ApplyRecordToContact populates.
func ParseVCF(reader io.Reader, db *gorm.DB, userID uint) (contacts []VCFContactData, previews []models.ImportRowPreview, stats ImportStats, err error) {
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		return nil, nil, stats, fmt.Errorf("failed to read VCF file: %w", readErr)
	}

	blocks := splitVCardBlocks(stripUTF8BOM(data))
	if len(blocks) == 0 {
		return nil, nil, stats, fmt.Errorf("VCF file contains no valid vCards")
	}

	// T96: prior rows' flat contacts, for within-batch duplicate detection.
	var batchContacts []*models.Contact

	for rowIdx, block := range blocks {
		if rowIdx >= MaxVCFContacts {
			return nil, nil, stats, fmt.Errorf("too many contacts: maximum is %d contacts", MaxVCFContacts)
		}

		record, diags, importErr := ImportVCardBlock(block)
		if importErr != nil {
			// Skip malformed vCards but continue parsing
			previews = append(previews, models.ImportRowPreview{
				RowIndex:         rowIdx,
				ParsedContact:    make(map[string]interface{}),
				ValidationErrors: []string{fmt.Sprintf("Failed to parse vCard: %v", importErr)},
				SuggestedAction:  "skip",
			})
			stats.ErrorCount++
			continue
		}

		contact := &models.Contact{}
		// photoDir is deliberately "" here: this is only a preview, not yet
		// confirmed by the user (see ApplyRecordToContact's photoDir doc
		// comment) — photo persistence happens later, at confirm time, via
		// extractPhotoFromRecord below + import_session.go's ConfirmVCF.
		models.ApplyRecordToContact(contact, record, "")

		// Generate UUID for contacts without one to avoid unique constraint violation
		if contact.VCardUID == "" {
			contact.VCardUID = uuid.New().String()
		}

		photoData, photoMediaType, photoURL := extractPhotoFromRecord(record)

		contacts = append(contacts, VCFContactData{
			Contact:        contact,
			PhotoData:      photoData,
			PhotoMediaType: photoMediaType,
			PhotoURL:       photoURL,
		})

		// T96: shared preview wiring (validation, DB duplicate detection +
		// merge diff, within-batch detection). batchContacts is appended AFTER
		// the call so a row is never compared against itself.
		preview := BuildImportRowPreview(db, userID, contact, rowIdx, batchContacts, diags, &stats)
		batchContacts = append(batchContacts, contact)

		previews = append(previews, preview)
	}

	if len(contacts) == 0 {
		return nil, nil, stats, fmt.Errorf("VCF file contains no valid contacts")
	}

	return contacts, previews, stats, nil
}

// ParseJSContact reads and parses a JSContact JSON import (
// extension: this import path is new, there is no legacy equivalent to
// replace). The file may be a single JSContact Card object, or a JSON array
// of Card objects (the same "Card set" shape ExportContactsAsJSContact
// produces) — array form is tried first, falling back to treating the whole
// payload as one Card. Mirrors ParseVCF's structure/UX (duplicate detection,
// validation, VCFContactData reuse so the existing ConfirmVCF confirmation
// pipeline — including photo processing — handles JSContact-imported
// contacts identically to VCF-imported ones without any changes there).
func ParseJSContact(reader io.Reader, db *gorm.DB, userID uint) (contacts []VCFContactData, previews []models.ImportRowPreview, stats ImportStats, err error) {
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		return nil, nil, stats, fmt.Errorf("failed to read JSContact file: %w", readErr)
	}

	data = stripUTF8BOM(data)
	var rawCards []json.RawMessage
	if jsonErr := json.Unmarshal(data, &rawCards); jsonErr != nil {
		rawCards = []json.RawMessage{json.RawMessage(data)}
	}
	if len(rawCards) == 0 {
		return nil, nil, stats, fmt.Errorf("JSContact file contains no cards")
	}

	adapter := jscontact.Adapter{}
	// T96: prior rows' flat contacts, for within-batch duplicate detection.
	var batchContacts []*models.Contact
	for rowIdx, raw := range rawCards {
		if rowIdx >= MaxVCFContacts {
			return nil, nil, stats, fmt.Errorf("too many contacts: maximum is %d contacts", MaxVCFContacts)
		}

		record, diags, importErr := adapter.Import(raw)
		if importErr != nil {
			previews = append(previews, models.ImportRowPreview{
				RowIndex:         rowIdx,
				ParsedContact:    make(map[string]interface{}),
				ValidationErrors: []string{fmt.Sprintf("Failed to parse JSContact card: %v", importErr)},
				SuggestedAction:  "skip",
			})
			stats.ErrorCount++
			continue
		}

		contact := &models.Contact{}
		// photoDir is deliberately "" here: this is only a preview, not yet
		// confirmed by the user (see ApplyRecordToContact's photoDir doc
		// comment) — photo persistence happens later, at confirm time, via
		// extractPhotoFromRecord below + import_session.go's ConfirmVCF.
		models.ApplyRecordToContact(contact, record, "")
		if contact.VCardUID == "" {
			contact.VCardUID = uuid.New().String()
		}

		photoData, photoMediaType, photoURL := extractPhotoFromRecord(record)

		contacts = append(contacts, VCFContactData{
			Contact:        contact,
			PhotoData:      photoData,
			PhotoMediaType: photoMediaType,
			PhotoURL:       photoURL,
		})

		// T96: shared preview wiring (validation, DB duplicate detection +
		// merge diff, within-batch detection).
		preview := BuildImportRowPreview(db, userID, contact, rowIdx, batchContacts, diags, &stats)
		batchContacts = append(batchContacts, contact)

		previews = append(previews, preview)
	}

	if len(contacts) == 0 {
		return nil, nil, stats, fmt.Errorf("JSContact file contains no valid contacts")
	}

	return contacts, previews, stats, nil
}

// ImportStats holds statistics about an import operation
type ImportStats struct {
	ValidCount     int
	DuplicateCount int
	ErrorCount     int
}

// headerToField holds case-insensitive rules for non-indexed headers.
var headerToField = map[string]string{
	// English
	"firstname": "firstname", "first name": "firstname", "first": "firstname", "given name": "firstname",
	"lastname": "lastname", "last name": "lastname", "last": "lastname", "surname": "lastname", "family name": "lastname",
	"middle name": "middle_name", "middle": "middle_name", "additional name": "middle_name",
	"name prefix": "prefix", "prefix": "prefix", "title prefix": "prefix",
	"name suffix": "suffix", "suffix": "suffix",
	"nickname": "nickname", "nick": "nickname", "alias": "nickname",
	"email": "email", "e-mail": "email", "mail": "email", "email address": "email",
	"phone": "phone", "telephone": "phone", "tel": "phone", "mobile": "phone", "cell": "phone", "phone number": "phone",
	"website": "url", "web site": "url", "url": "url", "homepage": "url",
	"birthday": "birthday", "birth date": "birthday", "birthdate": "birthday", "dob": "birthday", "date of birth": "birthday",
	"anniversary": "anniversary",
	"address":     "address_street", "street address": "address_street", "home address": "address_street", "street": "address_street",
	"city": "address_city", "town": "address_city",
	"region": "address_region", "state": "address_region", "province": "address_region",
	"postal code": "address_postal", "zip": "address_postal", "zip code": "address_postal", "postcode": "address_postal",
	"country": "address_country",
	"gender":  "gender", "sex": "gender",
	"organization": "organization", "organization name": "organization", "company": "organization", "employer": "organization",
	"department": "department", "organization department": "department",
	"job title": "job_title", "title": "job_title", "organization title": "job_title", "position": "job_title",
	"role": "role", "organization role": "role",
	"how we met": "how_we_met", "how_we_met": "how_we_met", "notes": "how_we_met", "how i met": "how_we_met",
	"work": "work_information", "work_information": "work_information", "job": "work_information", "occupation": "work_information",
	"contact information": "contact_information", "contact_information": "contact_information", "other contact": "contact_information",
	// Grouping vocabularies split by TARGET (T3), not all collapsed onto
	// "circles" as they were before Tag existed as a real destination:
	// a Circle is a group you belong to, a Tag is a label you carry.
	"circles": "circles", "groups": "circles",
	"tags": "tags", "labels": "tags", "category": "tags", "categories": "tags",
	// German
	"vorname":  "firstname",
	"nachname": "lastname", "familienname": "lastname",
	"zweiter vorname": "middle_name",
	"spitzname":       "nickname",
	"telefon":         "phone", "handy": "phone", "mobiltelefon": "phone",
	"webseite": "url", "website (de)": "url",
	"geburtstag": "birthday", "geburtsdatum": "birthday",
	"jahrestag": "anniversary",
	"adresse":   "address_street", "anschrift": "address_street", "straße": "address_street", "strasse": "address_street",
	"stadt": "address_city", "ort": "address_city",
	"bundesland": "address_region",
	"plz":        "address_postal", "postleitzahl": "address_postal",
	"land":       "address_country",
	"geschlecht": "gender",
	"firma":      "organization", "unternehmen": "organization",
	"abteilung": "department",
	"beruf":     "work_information", "arbeit": "work_information",
	"kreise": "circles", "gruppen": "circles",
}

// indexedHeaderRe matches Google-style grouped columns, e.g. "E-mail 1 - Value",
// "Phone 2 - Label", "Address 1 - Postal Code".
var indexedHeaderRe = regexp.MustCompile(`^(.+?)\s+(\d+)\s*-\s*(.+)$`)

func suggestGroupedMapping(header string) (field string, group int, ok bool) {
	m := indexedHeaderRe.FindStringSubmatch(strings.TrimSpace(header))
	if m == nil {
		return "", 0, false
	}
	base := strings.ToLower(strings.TrimSpace(m[1]))
	idx, err := strconv.Atoi(m[2])
	if err != nil || idx < 1 {
		return "", 0, false
	}
	attr := strings.ToLower(strings.TrimSpace(m[3]))

	// Identify the value family.
	var family string
	switch base {
	case "e-mail", "email":
		family = "email"
	case "phone", "telephone", "tel":
		family = "phone"
	case "website", "web site", "url":
		family = "url"
	case "im", "instant message", "instant messaging":
		family = "impp"
	case "address":
		family = "address"
	default:
		return "", 0, false
	}

	switch family {
	case "address":
		switch attr {
		case "street", "formatted", "po box", "extended address", "address":
			field = "address_street"
		case "city", "locality":
			field = "address_city"
		case "region", "state", "province":
			field = "address_region"
		case "postal code", "zip", "zip code", "postcode":
			field = "address_postal"
		case "country":
			field = "address_country"
		case "label", "type":
			field = "address_label"
		default:
			return "", 0, false
		}
	default:
		switch attr {
		case "value", "address", "uri":
			field = family
		case "label", "type":
			field = family + "_label"
		default:
			return "", 0, false
		}
	}

	return field, idx - 1, true
}

// SuggestColumnMappings guesses mappings based on CSV header names
func SuggestColumnMappings(headers []string) []models.ColumnMapping {
	mappings := make([]models.ColumnMapping, len(headers))

	for i, header := range headers {
		// indexed columns (e.g. "E-mail 1 - Value") take priority
		if field, group, ok := suggestGroupedMapping(header); ok {
			mappings[i] = models.ColumnMapping{CSVColumn: header, ContactField: field, Group: group}
			continue
		}

		normalized := strings.ToLower(strings.TrimSpace(header))
		if field, ok := headerToField[normalized]; ok {
			mappings[i] = models.ColumnMapping{CSVColumn: header, ContactField: field}
		} else {
			mappings[i] = models.ColumnMapping{CSVColumn: header, ContactField: ""} // Unmapped
		}
	}

	return mappings
}

// GenerateCSVPreview applies mappings to CSV rows
func GenerateCSVPreview(db *gorm.DB, userID uint, rows [][]string, headers []string, mappings []models.ColumnMapping) ([]models.Contact, []models.ImportRowPreview, ImportStats) {
	contacts := make([]models.Contact, len(rows))
	var previews []models.ImportRowPreview
	var stats ImportStats
	// T96: prior rows' flat contacts, for within-batch duplicate detection.
	// Points into preallocated contacts (no reallocation), so the pointers
	// stay valid for the whole loop.
	var batchContacts []*models.Contact

	for rowIdx, row := range rows {
		contact := BuildContactFromRow(userID, headers, row, mappings)
		contacts[rowIdx] = contact

		// T96: shared preview wiring (validation, DB duplicate detection +
		// merge diff, within-batch detection). CSV rows don't go through a
		// vCard adapter, so diags is nil.
		preview := BuildImportRowPreview(db, userID, &contacts[rowIdx], rowIdx, batchContacts, nil, &stats)
		batchContacts = append(batchContacts, &contacts[rowIdx])

		previews = append(previews, preview)
	}

	return contacts, previews, stats
}

// ContactToPreviewMap converts a Contact to a preview map used for display in the
// wizard and for diffing in merge notes. It carries the denormalized primary scalars
// plus the new structured fields; multi-value arrays are summarized by their primary.
func ContactToPreviewMap(contact *models.Contact) map[string]interface{} {
	preview := make(map[string]interface{})
	set := func(key, value string) {
		if value != "" {
			preview[key] = value
		}
	}
	set("firstname", contact.Firstname)
	set("lastname", contact.Lastname)
	set("middle_name", contact.MiddleName)
	set("prefix", contact.Prefix)
	set("suffix", contact.Suffix)
	set("nickname", contact.Nickname)
	set("email", contact.Email)
	set("phone", contact.Phone)
	set("birthday", contact.Birthday)
	set("anniversary", contact.Anniversary)
	set("address", contact.Address)
	set("gender", contact.Gender)
	set("organization", contact.Organization)
	set("department", contact.Department)
	set("job_title", contact.JobTitle)
	set("role", contact.Role)
	set("work_information", contact.WorkInformation)
	if len(contact.Circles) > 0 {
		preview["circles"] = strings.Join(contact.Circles, ", ")
	}
	if len(contact.ImportedTags) > 0 {
		preview["tags"] = strings.Join(contact.ImportedTags, ", ")
	}
	return preview
}

// sanitizedContactTextFields lists the free-text Contact fields hostile
// import input can reach (issue #416). Emails/Phones/Birthday/Anniversary
// are deliberately excluded: each already has its own format validator in
// ValidateImportedContact, which will reject a value corrupted by control
// characters or invalid UTF-8 on its own.
func sanitizedContactTextFields(contact *models.Contact) []*string {
	return []*string{
		&contact.Firstname,
		&contact.Lastname,
		&contact.Nickname,
		&contact.Gender,
		&contact.Address,
		&contact.HowWeMet,
		&contact.WorkInformation,
		&contact.ContactInformation,
	}
}

// sanitizeImportedText fixes the two byte-level hostilities a vCard/CSV/
// JSContact field can carry that cost nothing to fix: invalid UTF-8 (no
// legitimate vCard property is invalid UTF-8 -- it's either a decoding bug
// upstream or a deliberately hostile byte sequence) and C0/C1 control
// characters other than tab/LF/CR (real contact data doesn't contain a NUL
// byte or an ESC). Reports whether it changed anything, so the caller can
// attach a diagnostic without guessing.
//
// Deliberately NOT here: length truncation or HTML/script stripping. Both
// would silently narrow real user content -- this repo's production-data
// rule ("breaking data needs a reason", CLAUDE.md) and ADR-0002's
// "preserve, don't reject" policy both argue against it, and the frontend
// renders every free-text field via plain JSX interpolation (no
// dangerouslySetInnerHTML, no markdown rendering anywhere in frontend/src),
// so HTML/script in a field is inert on render -- there is no risk here to
// spend real user data closing.
func sanitizeImportedText(s string) (string, bool) {
	cleaned := strings.ToValidUTF8(s, "�")
	var b strings.Builder
	changed := cleaned != s
	b.Grow(len(cleaned))
	for _, r := range cleaned {
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(r)
			continue
		}
		// C0 (0x00-0x1F) and C1 (0x7F-0x9F) control characters.
		if (r >= 0x00 && r <= 0x1F) || (r >= 0x7F && r <= 0x9F) {
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

// SanitizeImportedContact applies sanitizeImportedText to every free-text
// field an import path can populate, mutating contact in place, and returns
// one short info Diagnostic per field actually changed (nil when nothing
// changed). Called from BuildImportRowPreview -- the single choke point
// shared by CSV, VCF, JSContact, and the Android "records" import paths --
// before ValidateImportedContact runs, so format validators never have to
// deal with control characters or invalid UTF-8 in the first place.
func SanitizeImportedContact(contact *models.Contact) []contactmodel.Diagnostic {
	var notes []contactmodel.Diagnostic
	fieldNames := []string{"firstname", "lastname", "nickname", "gender", "address", "how_we_met", "work_information", "contact_information"}
	for i, field := range sanitizedContactTextFields(contact) {
		if cleaned, changed := sanitizeImportedText(*field); changed {
			*field = cleaned
			notes = append(notes, contactmodel.Diagnostic{
				Severity: "info",
				Message:  fmt.Sprintf("Removed invalid characters from %s", fieldNames[i]),
			})
		}
	}
	if sanitizeCardName(contact) {
		notes = append(notes, contactmodel.Diagnostic{
			Severity: "info",
			Message:  "Removed invalid characters from name",
		})
	}
	return notes
}

// sanitizeCardName applies sanitizeImportedText to contact.Card.Name -- the
// neutral full-fidelity copy ApplyRecordToContact populates alongside the
// flat Firstname/Lastname/FN fields the loop above cleans. This is not
// redundant with that loop: Contact.BeforeSave's cardSetDirectly branch
// (models/contact.go) re-derives Firstname/Lastname/FN from contact.Card on
// every save that follows an ApplyRecordToContact call -- VCF/JSContact
// import confirm, CardDAV/CalDAV reconcile, and the REST API's
// CreateContact/UpdateContact -- so cleaning only the flat fields and
// leaving contact.Card.Name untouched would have that re-derivation quietly
// undo the sanitization above the moment the contact is actually saved. A
// real, hand-verified bug (not hypothetical): issue #512's CardDAV
// hostile-input E2E coverage caught this by asserting against the saved,
// reloaded row rather than the in-memory contact right after this function
// returns -- see contact_sync_hostile_input_test.go.
func sanitizeCardName(contact *models.Contact) bool {
	if contact == nil || contact.Card.Name == nil {
		return false
	}
	changed := false
	if cleaned, ch := sanitizeImportedText(contact.Card.Name.Full); ch {
		contact.Card.Name.Full = cleaned
		changed = true
	}
	for i := range contact.Card.Name.Components {
		if cleaned, ch := sanitizeImportedText(contact.Card.Name.Components[i].Value); ch {
			contact.Card.Name.Components[i].Value = cleaned
			changed = true
		}
	}
	return changed
}

// ValidateImportedContact validates a contact built from either CSV or VCF and returns
// human-readable errors. Used by both import preview paths.
func ValidateImportedContact(contact *models.Contact) []string {
	errors := make([]string, 0)

	if contact.Firstname == "" {
		errors = append(errors, "First name is required")
	}

	for _, e := range contact.Emails {
		if e.Value != "" && !middleware.ValidateEmail(e.Value) {
			errors = append(errors, "Invalid email format")
			break
		}
	}

	for _, p := range contact.Phones {
		if p.Value != "" && !IsValidPhone(p.Value) {
			errors = append(errors, "Invalid phone format")
			break
		}
	}

	if contact.Birthday != "" && !IsValidBirthdayFormat(contact.Birthday) {
		errors = append(errors, "Invalid birthday format (expected YYYY-MM-DD or --MM-DD)")
	}

	if contact.Anniversary != "" && !IsValidBirthdayFormat(contact.Anniversary) {
		errors = append(errors, "Invalid anniversary format (expected YYYY-MM-DD or --MM-DD)")
	}

	return errors
}

// GetStringField safely gets a string field from parsed contact
func GetStringField(parsed map[string]interface{}, field string) string {
	if val, ok := parsed[field]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// IsValidBirthdayFormat checks birthday format (YYYY-MM-DD or --MM-DD)
func IsValidBirthdayFormat(birthday string) bool {
	match, _ := regexp.MatchString(`^(--|\d{4}-)\d{2}-\d{2}$`, birthday)
	return match
}

// NormalizeBirthday converts various birthday formats to the app's ISO format (YYYY-MM-DD or --MM-DD)
// Supported input formats:
// - YYYY-MM-DD (ISO format with year, e.g., "1958-06-29") - native format
// - --MM-DD (ISO format without year, e.g., "--04-20") - native format
// - DD.MM.YYYY (legacy format with year, e.g., "29.06.1958")
// - DD.MM. (legacy format without year, e.g., "29.06.")
func NormalizeBirthday(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Already in ISO format with year: YYYY-MM-DD - return as-is
	if match, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, input); match {
		return input
	}

	// Already in ISO format without year: --MM-DD - return as-is
	if match, _ := regexp.MatchString(`^--\d{2}-\d{2}$`, input); match {
		return input
	}

	// Legacy format with year: DD.MM.YYYY -> YYYY-MM-DD
	if match, _ := regexp.MatchString(`^\d{2}\.\d{2}\.\d{4}$`, input); match {
		day := input[0:2]
		month := input[3:5]
		year := input[6:10]
		return year + "-" + month + "-" + day
	}

	// Legacy format without year: DD.MM. -> --MM-DD
	if match, _ := regexp.MatchString(`^\d{2}\.\d{2}\.$`, input); match {
		day := input[0:2]
		month := input[3:5]
		return "--" + month + "-" + day
	}

	// Unknown format - return as-is (will fail validation)
	return input
}

// IsValidPhone validates phone number format
func IsValidPhone(phone string) bool {
	// Remove common formatting characters
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, phone)

	// Must have between 5 and 20 digits
	return len(cleaned) >= 5 && len(cleaned) <= 20
}

// NormalizeGender canonicalizes common gender inputs to their standard form
// for consistent display. Unrecognized values are passed through unchanged
// (gender is free-text in this project).
func NormalizeGender(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	switch lower {
	case "m", "male", "mann", "maennlich", "männlich", "masculin":
		return "male"
	case "f", "female", "frau", "weiblich", "feminin", "w":
		return "female"
	case "o", "other", "andere", "divers", "d":
		return "other"
	case "prefer not to say", "prefer_not_to_say", "keine angabe":
		return "other"
	case "non-binary", "nonbinary", "non binaire", "nichtbinär", "nicht binär", "no binario", "non binario":
		return "non_binary"
	case "genderfluid", "gender fluid", "genre fluide", "género fluido", "genere fluido":
		return "genderfluid"
	default:
		return input
	}
}

// DetectDuplicate checks for existing contacts matching the given fields.
//
// The name/email comparisons are byte keys (SQLite LOWER + LOWER(param)), so
// the incoming values are NFC-folded here before they reach those keys —
// the duplicate-detection half of I18N-02 (issue #485): an NFD spelling must
// match an NFC-stored spelling. (Stored data is NFC by the write boundary;
// this makes the detector robust to any caller that has not already gone
// through ApplyRecordToContact.)
func DetectDuplicate(db *gorm.DB, userID uint, firstname, lastname, email, phone string) *models.DuplicateMatch {
	firstname = norm.NFC.String(firstname)
	lastname = norm.NFC.String(lastname)
	email = norm.NFC.String(email)
	var existing models.Contact

	// Priority 1: Email match (if email provided)
	if email != "" {
		if err := db.Where("user_id = ? AND LOWER(email) = LOWER(?)", userID, email).First(&existing).Error; err == nil {
			return &models.DuplicateMatch{
				ExistingContactID: existing.ID,
				ExistingFirstname: existing.Firstname,
				ExistingLastname:  existing.Lastname,
				ExistingEmail:     existing.Email,
				ExistingPhone:     existing.Phone,
				MatchReason:       "email",
			}
		}
	}

	// Priority 2: Name match (firstname + lastname)
	if firstname != "" && lastname != "" {
		if err := db.Where("user_id = ? AND LOWER(firstname) = LOWER(?) AND LOWER(lastname) = LOWER(?)",
			userID, firstname, lastname).First(&existing).Error; err == nil {
			return &models.DuplicateMatch{
				ExistingContactID: existing.ID,
				ExistingFirstname: existing.Firstname,
				ExistingLastname:  existing.Lastname,
				ExistingEmail:     existing.Email,
				ExistingPhone:     existing.Phone,
				MatchReason:       "name",
			}
		}
	}

	// Priority 3: Phone match (if phone provided)
	// Normalize phone numbers to a canonical last-10-digit key (PhoneKey) so
	// that +1-country-code, trunk-prefix and punctuation-only differences
	// still detect a duplicate. An empty key means the number is too short to
	// match — treat it the same as "no phone provided."
	if phone != "" {
		phoneKey := models.PhoneKey(phone)
		if phoneKey != "" {
			var contacts []models.Contact
			if err := db.Where("user_id = ? AND phone != ''", userID).Find(&contacts).Error; err == nil {
				for _, c := range contacts {
					if models.PhoneKey(c.Phone) == phoneKey {
						return &models.DuplicateMatch{
							ExistingContactID: c.ID,
							ExistingFirstname: c.Firstname,
							ExistingLastname:  c.Lastname,
							ExistingEmail:     c.Email,
							ExistingPhone:     c.Phone,
							MatchReason:       "phone",
						}
					}
				}
			}
		}
	}

	return nil
}

// ParseCircles parses circles from a separated string
func ParseCircles(input string) []string {
	if input == "" {
		return nil
	}

	// Normalize ":::" separator to a comma so the splitter below handles it.
	normalized := strings.ReplaceAll(input, ":::", ",")

	var circles []string
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == ',' || r == ';'
	})

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || strings.HasPrefix(trimmed, "*") {
			continue
		}
		circles = append(circles, trimmed)
	}

	return circles
}

// addrEntry accumulates the components of one structured address while building a row.
type addrEntry struct {
	label   string
	street  string
	city    string
	region  string
	postal  string
	country string
}

func (a addrEntry) isEmpty() bool {
	return strings.TrimSpace(a.street+a.city+a.region+a.postal+a.country) == ""
}

// BuildContactFromRow assembles a full multi-value Contact from a single CSV row using
// the column mappings. Scalars are set directly; value/label/part columns sharing a
// (family, Group) assemble into one ContactEmail/Phone/Address/URL/IMPP entry.
func BuildContactFromRow(userID uint, headers []string, row []string, mappings []models.ColumnMapping) models.Contact {
	columnIndex := make(map[string]int, len(headers))
	for i, header := range headers {
		columnIndex[header] = i
	}

	// cellValue returns the trimmed value for a mapped column, or "" when out of range.
	cellValue := func(m models.ColumnMapping) string {
		idx, ok := columnIndex[m.CSVColumn]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	contact := models.Contact{UserID: userID}

	// Multi-value accumulators keyed by group index. Ordered slices preserve appearance.
	emailVals := map[int]string{}
	emailLabels := map[int]string{}
	phoneVals := map[int]string{}
	phoneLabels := map[int]string{}
	urlVals := map[int]string{}
	urlLabels := map[int]string{}
	imppVals := map[int]string{}
	imppLabels := map[int]string{}
	addrs := map[int]*addrEntry{}
	var emailGroups, phoneGroups, urlGroups, imppGroups, addrGroups []int

	addrFor := func(g int) *addrEntry {
		if a, ok := addrs[g]; ok {
			return a
		}
		a := &addrEntry{}
		addrs[g] = a
		addrGroups = append(addrGroups, g)
		return a
	}
	// if two columns manually mapped to same value like "Email", it bumps to the
	// next free group so both values survive rather than overwriting
	putValue := func(vals map[int]string, order *[]int, g int, v string) {
		if v == "" {
			return
		}
		if cur, ok := vals[g]; ok && cur != "" {
			for {
				g++
				if _, taken := vals[g]; !taken {
					break
				}
			}
		}
		if _, seen := vals[g]; !seen {
			*order = append(*order, g)
		}
		vals[g] = v
	}

	for _, m := range mappings {
		if m.ContactField == "" {
			continue
		}
		v := cellValue(m)
		switch m.ContactField {
		case "firstname":
			contact.Firstname = v
		case "lastname":
			contact.Lastname = v
		case "middle_name":
			contact.MiddleName = v
		case "prefix":
			contact.Prefix = v
		case "suffix":
			contact.Suffix = v
		case "nickname":
			contact.Nickname = v
		case "gender":
			if v != "" {
				contact.Gender = NormalizeGender(v)
			}
		case "birthday":
			if v != "" {
				contact.Birthday = NormalizeBirthday(v)
			}
		case "anniversary":
			if v != "" {
				contact.Anniversary = NormalizeBirthday(v)
			}
		case "organization":
			contact.Organization = v
		case "department":
			contact.Department = v
		case "job_title":
			contact.JobTitle = v
		case "role":
			contact.Role = v
		case "how_we_met":
			contact.HowWeMet = v
		case "work_information":
			contact.WorkInformation = v
		case "contact_information":
			contact.ContactInformation = v
		case "circles":
			if v != "" {
				contact.Circles = ParseCircles(v)
			}
		case "tags":
			// Staged on the non-persisted ImportedTags field; materialized into
			// real Tag + ContactTag rows once the contact has a VCardUID (see
			// services.MaterializeImportedGroupings).
			if v != "" {
				contact.ImportedTags = ParseCircles(v)
			}
		case "email":
			putValue(emailVals, &emailGroups, m.Group, v)
		case "email_label":
			emailLabels[m.Group] = v
		case "phone":
			putValue(phoneVals, &phoneGroups, m.Group, v)
		case "phone_label":
			phoneLabels[m.Group] = v
		case "url":
			putValue(urlVals, &urlGroups, m.Group, v)
		case "url_label":
			urlLabels[m.Group] = v
		case "impp":
			putValue(imppVals, &imppGroups, m.Group, v)
		case "impp_label":
			imppLabels[m.Group] = v
		case "address_street":
			addrFor(m.Group).street = v
		case "address_city":
			addrFor(m.Group).city = v
		case "address_region":
			addrFor(m.Group).region = v
		case "address_postal":
			addrFor(m.Group).postal = v
		case "address_country":
			addrFor(m.Group).country = v
		case "address_label":
			addrFor(m.Group).label = v
		}
	}

	for _, g := range emailGroups {
		if v := emailVals[g]; v != "" {
			contact.Emails = append(contact.Emails, models.ContactEmail{Type: normalizeImportType(emailLabels[g], "home"), Value: v})
		}
	}
	for _, g := range phoneGroups {
		if v := phoneVals[g]; v != "" {
			contact.Phones = append(contact.Phones, models.ContactPhone{Type: normalizeImportType(phoneLabels[g], "cell"), Value: v})
		}
	}
	for _, g := range urlGroups {
		if v := urlVals[g]; v != "" {
			contact.URLs = append(contact.URLs, models.ContactURL{Type: normalizeImportType(urlLabels[g], "home"), Value: v})
		}
	}
	for _, g := range imppGroups {
		if v := imppVals[g]; v != "" {
			contact.IMPPs = append(contact.IMPPs, models.ContactIMPP{Type: normalizeImportType(imppLabels[g], ""), Value: v})
		}
	}
	for _, g := range addrGroups {
		a := addrs[g]
		if a.isEmpty() {
			continue
		}
		contact.Addresses = append(contact.Addresses, models.ContactAddress{
			Type:    normalizeImportType(a.label, "home"),
			Street:  a.street,
			City:    a.city,
			Region:  a.region,
			Postal:  a.postal,
			Country: a.country,
		})
	}

	// Mirror the primary entries into the denormalized scalars so duplicate detection works
	if len(contact.Emails) > 0 {
		contact.Email = contact.Emails[0].Value
	}
	if len(contact.Phones) > 0 {
		contact.Phone = contact.Phones[0].Value
	}
	if len(contact.Addresses) > 0 {
		contact.Address = models.FormatAddress(contact.Addresses[0])
	}

	return contact
}

// clean up a type/label token (e.g. Google's "* Home")
func normalizeImportType(label, def string) string {
	t := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(label), "*")))
	switch t {
	case "":
		return def
	case "mobile":
		return "cell"
	default:
		return t
	}
}

// mergeContactValues implements T49's additive merge semantics for a
// multi-valued vCard field (Emails/Phones/Addresses/URLs/IMPPs): every
// existing entry survives unconditionally, and an incoming entry is appended
// only if it actually carries content (per isBlank) and isn't already present
// among the existing entries (per key, so a re-import that repeats a value
// the contact already has doesn't create a duplicate). This replaces the old
// "if len(incoming) > 0 { existing = incoming }" policy, which wholesale
// replaced the existing array — including with an incoming entry whose only
// content was a blank value (T50) — any time the incoming side had a
// nonzero-length slice at all. added mirrors exactly the subset of incoming
// that got appended, for callers (CreateMergeNote) that need to describe the
// merge without performing it.
func mergeContactValues[T any](existing, incoming []T, isBlank func(T) bool, key func(T) string) (merged []T, added []T) {
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		if !isBlank(e) {
			seen[key(e)] = struct{}{}
		}
	}
	merged = make([]T, len(existing), len(existing)+len(incoming))
	copy(merged, existing)
	for _, in := range incoming {
		if isBlank(in) {
			continue
		}
		k := key(in)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		merged = append(merged, in)
		added = append(added, in)
	}
	return merged, added
}

// displayValues renders a slice of merged/added entries for a merge-note line.
func displayValues[T any](vals []T, display func(T) string) []string {
	if len(vals) == 0 {
		return nil
	}
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = display(v)
	}
	return out
}

// isBlank/key/display helpers for each multi-valued Contact field, shared by
// MergeImportedContact and CreateMergeNote so both agree on what counts as
// "content" and what counts as "the same entry".
func contactEmailBlank(e models.ContactEmail) bool { return strings.TrimSpace(e.Value) == "" }
func contactEmailKey(e models.ContactEmail) string {
	return strings.ToLower(strings.TrimSpace(e.Value))
}
func contactEmailDisplay(e models.ContactEmail) string { return strings.TrimSpace(e.Value) }

func contactPhoneBlank(p models.ContactPhone) bool     { return strings.TrimSpace(p.Value) == "" }
func contactPhoneKey(p models.ContactPhone) string     { return strings.TrimSpace(p.Value) }
func contactPhoneDisplay(p models.ContactPhone) string { return strings.TrimSpace(p.Value) }

func contactURLBlank(u models.ContactURL) bool     { return strings.TrimSpace(u.Value) == "" }
func contactURLKey(u models.ContactURL) string     { return strings.ToLower(strings.TrimSpace(u.Value)) }
func contactURLDisplay(u models.ContactURL) string { return strings.TrimSpace(u.Value) }

func contactIMPPBlank(i models.ContactIMPP) bool { return strings.TrimSpace(i.Value) == "" }
func contactIMPPKey(i models.ContactIMPP) string {
	return strings.ToLower(strings.TrimSpace(i.Value))
}
func contactIMPPDisplay(i models.ContactIMPP) string { return strings.TrimSpace(i.Value) }

// ContactAddress has no single Value field, so blank/key/display are derived
// from FormatAddress's rendering of the whole struct: blank means every
// component is blank, and two addresses are "the same entry" if they render
// identically.
func contactAddressBlank(a models.ContactAddress) bool { return models.FormatAddress(a) == "" }
func contactAddressKey(a models.ContactAddress) string {
	return strings.ToLower(models.FormatAddress(a))
}
func contactAddressDisplay(a models.ContactAddress) string { return models.FormatAddress(a) }

// merges fields from an imported contact into an existing one, overwriting only non-empty incoming values
func MergeImportedContact(existing *models.Contact, incoming *models.Contact) {
	if incoming.Firstname != "" {
		existing.Firstname = incoming.Firstname
	}
	if incoming.Lastname != "" {
		existing.Lastname = incoming.Lastname
	}
	if incoming.Nickname != "" {
		existing.Nickname = incoming.Nickname
	}
	if incoming.Email != "" {
		existing.Email = incoming.Email
	}
	if incoming.Phone != "" {
		existing.Phone = incoming.Phone
	}
	if incoming.Birthday != "" {
		existing.Birthday = incoming.Birthday
	}
	if incoming.Address != "" {
		existing.Address = incoming.Address
	}
	if incoming.Gender != "" {
		existing.Gender = incoming.Gender
	}
	if incoming.WorkInformation != "" {
		existing.WorkInformation = incoming.WorkInformation
	}
	if incoming.HowWeMet != "" {
		existing.HowWeMet = incoming.HowWeMet
	}
	if incoming.ContactInformation != "" {
		existing.ContactInformation = incoming.ContactInformation
	}
	if len(incoming.Circles) > 0 {
		existing.Circles = incoming.Circles
	}
	// ImportedTags is carried across so an "update" row materializes its tags
	// too. Unlike Circles this is not persisted on the contact — it only needs
	// to survive as far as MaterializeImportedGroupings, which is additive, so
	// an update adds tags rather than replacing the contact's existing ones.
	if len(incoming.ImportedTags) > 0 {
		existing.ImportedTags = incoming.ImportedTags
	}
	// Multi-valued and structured vCard fields: additive merge (T49), not
	// replace. See mergeContactValues's doc comment for why "incoming has any
	// entries at all" was never the right trigger.
	existing.Emails, _ = mergeContactValues(existing.Emails, incoming.Emails, contactEmailBlank, contactEmailKey)
	existing.Phones, _ = mergeContactValues(existing.Phones, incoming.Phones, contactPhoneBlank, contactPhoneKey)
	existing.Addresses, _ = mergeContactValues(existing.Addresses, incoming.Addresses, contactAddressBlank, contactAddressKey)
	existing.URLs, _ = mergeContactValues(existing.URLs, incoming.URLs, contactURLBlank, contactURLKey)
	existing.IMPPs, _ = mergeContactValues(existing.IMPPs, incoming.IMPPs, contactIMPPBlank, contactIMPPKey)
	if incoming.MiddleName != "" {
		existing.MiddleName = incoming.MiddleName
	}
	if incoming.Prefix != "" {
		existing.Prefix = incoming.Prefix
	}
	if incoming.Suffix != "" {
		existing.Suffix = incoming.Suffix
	}
	if incoming.Organization != "" {
		existing.Organization = incoming.Organization
	}
	if incoming.Department != "" {
		existing.Department = incoming.Department
	}
	if incoming.JobTitle != "" {
		existing.JobTitle = incoming.JobTitle
	}
	if incoming.Role != "" {
		existing.Role = incoming.Role
	}
	if incoming.Anniversary != "" {
		existing.Anniversary = incoming.Anniversary
	}
	if incoming.VCardExtra != "" {
		existing.VCardExtra = incoming.VCardExtra
	}
	// existing.VCardUID is deliberately never touched here (T49): it is the
	// existing contact's identity, assigned once at its own creation, and
	// every graph-adjacent table (Gift, LifeEvent, RelationshipEdge, ...)
	// keys on it via entity_id. ParseVCF mints a fresh random UUID for any
	// source vCard lacking its own UID (the common case for real-world
	// exports), so accepting incoming.VCardUID here silently reassigns the
	// existing contact's identity and orphans every row filed under the old
	// one -- exactly what "gifts was also wiped out" turned out to be.
}

// importMergeDiffScalars is the scalar table backing ComputeImportMergeDiff,
// mirroring MergeImportedContact's overwrite set so the preview's "what Merge
// will do" and the confirm path's actual behavior cannot drift. Email/Phone/
// Address are deliberately absent: they are BeforeSave projections of
// Emails[0]/Phones[0]/Addresses[0] (see contact_merge_service.go's
// mergeScalarFields for the same reasoning), so a "changed email" is really an
// "added email" and is reported that way by the multi-value diff below.
// VCardExtra is likewise absent -- raw vCard payload, no user-facing meaning.
var importMergeDiffScalars = []struct {
	Key   string
	Label string
	Get   func(*models.Contact) string
}{
	{"firstname", "First Name", func(c *models.Contact) string { return c.Firstname }},
	{"lastname", "Last Name", func(c *models.Contact) string { return c.Lastname }},
	{"middle_name", "Middle Name", func(c *models.Contact) string { return c.MiddleName }},
	{"prefix", "Prefix", func(c *models.Contact) string { return c.Prefix }},
	{"suffix", "Suffix", func(c *models.Contact) string { return c.Suffix }},
	{"nickname", "Nickname", func(c *models.Contact) string { return c.Nickname }},
	{"gender", "Gender", func(c *models.Contact) string { return c.Gender }},
	{"birthday", "Birthday", func(c *models.Contact) string { return c.Birthday }},
	{"anniversary", "Anniversary", func(c *models.Contact) string { return c.Anniversary }},
	{"organization", "Organization", func(c *models.Contact) string { return c.Organization }},
	{"department", "Department", func(c *models.Contact) string { return c.Department }},
	{"job_title", "Job Title", func(c *models.Contact) string { return c.JobTitle }},
	{"role", "Role", func(c *models.Contact) string { return c.Role }},
	{"how_we_met", "How We Met", func(c *models.Contact) string { return c.HowWeMet }},
	{"work_information", "Work Information", func(c *models.Contact) string { return c.WorkInformation }},
	{"contact_information", "Contact Information", func(c *models.Contact) string { return c.ContactInformation }},
}

// ComputeImportMergeDiff returns, for a duplicate import row, exactly what the
// "Merge" (update) action will change on the existing contact: scalars that
// MergeImportedContact will overwrite (incoming wins when non-empty, existing
// survives when blank) and multi-valued entries it will append (the additive
// T49 merge). It is pure and shares MergeImportedContact's own helpers --
// mergeContactValues for the arrays, the same overwrite-when-non-empty rule
// for the scalars -- so the preview can never describe a merge the commit
// would not perform (pinned by a test that applies MergeImportedContact and
// asserts the diff predicted every change). Circles/Tags are deliberately not
// in the diff: membership materialization (MaterializeImportedGroupings) is
// additive and idempotent, and the flat Contact.Circles staging column is not
// a faithful record of an existing contact's real memberships, so comparing
// against it would be inaccurate.
func ComputeImportMergeDiff(existing, incoming *models.Contact) models.ImportMergeDiff {
	// Initialize both slices empty rather than nil: a nil slice serializes as
	// JSON `null` (Go encodes nil slices as null even without omitempty), and
	// the client renders diff.updated.length / diff.added.length directly --
	// CLAUDE.md frontend trap #8, the whole reason the diff must always carry
	// `[]`, never null.
	diff := models.ImportMergeDiff{
		Updated: []models.ImportScalarChange{},
		Added:   []models.ImportAddedValue{},
	}
	for _, f := range importMergeDiffScalars {
		oldVal, newVal := f.Get(existing), f.Get(incoming)
		if newVal != "" && newVal != oldVal {
			diff.Updated = append(diff.Updated, models.ImportScalarChange{
				Field: f.Key, Label: f.Label, Old: oldVal, New: newVal,
			})
		}
	}

	addAdded := func(kind string, added []string) {
		for _, v := range added {
			diff.Added = append(diff.Added, models.ImportAddedValue{Kind: kind, Value: v})
		}
	}
	_, addedEmails := mergeContactValues(existing.Emails, incoming.Emails, contactEmailBlank, contactEmailKey)
	addAdded("email", displayValues(addedEmails, contactEmailDisplay))
	_, addedPhones := mergeContactValues(existing.Phones, incoming.Phones, contactPhoneBlank, contactPhoneKey)
	addAdded("phone", displayValues(addedPhones, contactPhoneDisplay))
	_, addedAddresses := mergeContactValues(existing.Addresses, incoming.Addresses, contactAddressBlank, contactAddressKey)
	addAdded("address", displayValues(addedAddresses, contactAddressDisplay))
	_, addedURLs := mergeContactValues(existing.URLs, incoming.URLs, contactURLBlank, contactURLKey)
	addAdded("url", displayValues(addedURLs, contactURLDisplay))
	_, addedIMPPs := mergeContactValues(existing.IMPPs, incoming.IMPPs, contactIMPPBlank, contactIMPPKey)
	addAdded("impp", displayValues(addedIMPPs, contactIMPPDisplay))

	return diff
}

// loadImportDuplicate loads the full flat Contact a DuplicateMatch points at,
// so the preview can compute what merging into it would change. The match
// itself came from DetectDuplicate, which is already user-scoped; the load
// re-scopes by user_id as defense in depth (CLAUDE.md backend trap #5).
// Returns nil when the row vanished between detection and load (e.g. a
// concurrent delete) -- the caller then shows the match without a diff.
func loadImportDuplicate(db *gorm.DB, userID uint, id uint) *models.Contact {
	var existing models.Contact
	if err := db.Where("user_id = ?", userID).First(&existing, id).Error; err != nil {
		return nil
	}
	return &existing
}

// batchDuplicateIndex returns the index of the earliest earlier row in batch
// that duplicates candidate, or -1. T96's within-batch detection: the same
// person imported twice in one file. Same tier order and key normalizers as
// DetectDuplicate (email, name, phone via PhoneKey), but compared against
// sibling rows of the same import rather than the whole contacts table, and
// reading the full multi-value arrays rather than just the flat primaries.
func batchDuplicateIndex(batch []*models.Contact, candidate *models.Contact) int {
	for i, prev := range batch {
		if contactsMatchWithinBatch(prev, candidate) {
			return i
		}
	}
	return -1
}

// contactsMatchWithinBatch reports whether two import rows are the same person
// by any of DetectDuplicate's three tiers: a shared email (case-insensitive),
// an exact shared firstname+lastname (both non-empty), or a phone reducing to
// the same PhoneKey (T68, so +1-country-code / punctuation differences still
// match).
func contactsMatchWithinBatch(a, b *models.Contact) bool {
	aEmails := map[string]bool{}
	for _, e := range a.Emails {
		if v := strings.ToLower(strings.TrimSpace(e.Value)); v != "" {
			aEmails[v] = true
		}
	}
	for _, e := range b.Emails {
		if v := strings.ToLower(strings.TrimSpace(e.Value)); v != "" && aEmails[v] {
			return true
		}
	}

	aFN := strings.ToLower(norm.NFC.String(strings.TrimSpace(a.Firstname)))
	aLN := strings.ToLower(norm.NFC.String(strings.TrimSpace(a.Lastname)))
	bFN := strings.ToLower(norm.NFC.String(strings.TrimSpace(b.Firstname)))
	bLN := strings.ToLower(norm.NFC.String(strings.TrimSpace(b.Lastname)))
	if aFN != "" && aLN != "" && aFN == bFN && aLN == bLN {
		return true
	}

	aPhones := map[string]bool{}
	for _, p := range a.Phones {
		if k := models.PhoneKey(p.Value); k != "" {
			aPhones[k] = true
		}
	}
	for _, p := range b.Phones {
		if k := models.PhoneKey(p.Value); k != "" && aPhones[k] {
			return true
		}
	}
	return false
}

// BuildImportRowPreview finalizes a successfully-parsed import row into its
// preview row: validation, DB duplicate detection (with the per-row merge
// diff, ComputeImportMergeDiff) and T96 within-batch detection against the
// sibling contacts built so far, updating stats accordingly. contact is the
// flat Contact for this row; batch holds every EARLIER row's contact in row
// order (the caller appends contact after the call, so a row is never
// compared against itself); diags is the row's adapter diagnostics (nil for
// CSV/records rows, which don't go through an adapter). Shared by the VCF,
// JSContact, CSV and records-import preview builders so the
// duplicate/diff/default-action wiring cannot drift between formats.
func BuildImportRowPreview(
	db *gorm.DB,
	userID uint,
	contact *models.Contact,
	rowIdx int,
	batch []*models.Contact,
	diags []contactmodel.Diagnostic,
	stats *ImportStats,
) models.ImportRowPreview {
	// Issue #416: fix up invalid UTF-8/control characters before the
	// preview is built or validated, so both reflect the cleaned value and
	// the format validators below never have to reason about hostile bytes.
	diags = append(diags, SanitizeImportedContact(contact)...)

	preview := models.ImportRowPreview{
		RowIndex:              rowIdx,
		ParsedContact:         ContactToPreviewMap(contact),
		ValidationErrors:      ValidateImportedContact(contact),
		Diagnostics:           diags,
		SuggestedAction:       "add",
		CustomFieldCandidates: DiscoverCustomFieldCandidates(db, userID, contact),
	}

	if len(preview.ValidationErrors) > 0 {
		stats.ErrorCount++
		preview.SuggestedAction = "skip"
		return preview
	}
	stats.ValidCount++

	// Within-batch detection: does this row duplicate an earlier row of the
	// same file? Defaults to skip so the twin is never created; the user may
	// override to Keep Both.
	if idx := batchDuplicateIndex(batch, contact); idx >= 0 {
		preview.BatchDuplicateOf = &idx
		preview.SuggestedAction = "skip"
	}

	// DB duplicate detection -- the import path's own detector
	// (DetectDuplicate), unchanged. Only a row with no within-batch match
	// defaults to update; a row that duplicates both a sibling and an existing
	// record stays skip-by-default so the file's twin isn't created twice.
	duplicate := DetectDuplicate(db, userID, contact.Firstname, contact.Lastname, contact.Email, contact.Phone)
	if duplicate != nil {
		preview.DuplicateMatch = duplicate
		if existing := loadImportDuplicate(db, userID, duplicate.ExistingContactID); existing != nil {
			diff := ComputeImportMergeDiff(existing, contact)
			preview.MergeDiff = &diff
		}
		if preview.BatchDuplicateOf == nil {
			preview.SuggestedAction = "update"
		}
		stats.DuplicateCount++
	}
	return preview
}

// creates a note documenting what an "update" import actually changed on an
// existing contact. incoming is the parsed import row's own Contact (not yet
// merged into original -- both call sites run this before
// MergeImportedContact). Scalar fields are reported the same way as before
// (old -> new); multi-valued fields (Emails/Phones/Addresses/URLs/IMPPs) are
// reported as "added" lists (T49): now that merging those fields is
// additive rather than replace, an old-value/new-value diff of the whole
// array no longer describes what happened -- only what got appended does.
// Email/Phone/Address are intentionally not reported as scalars: they are
// just the denormalized "first entry" projection of Emails/Phones/Addresses
// (Contact.BeforeSave), already covered by the array reporting below.
func CreateMergeNote(db *gorm.DB, userID uint, contactID uint, original *models.Contact, incoming *models.Contact, importType string) error {
	var changes []string

	addScalar := func(label, oldVal, newVal string) {
		if newVal != "" && oldVal != newVal {
			if oldVal != "" {
				changes = append(changes, fmt.Sprintf("- %s: %s → %s", label, oldVal, newVal))
			} else {
				changes = append(changes, fmt.Sprintf("- %s: (empty) → %s", label, newVal))
			}
		}
	}

	addScalar("First Name", original.Firstname, incoming.Firstname)
	addScalar("Last Name", original.Lastname, incoming.Lastname)
	addScalar("Middle Name", original.MiddleName, incoming.MiddleName)
	addScalar("Prefix", original.Prefix, incoming.Prefix)
	addScalar("Suffix", original.Suffix, incoming.Suffix)
	addScalar("Nickname", original.Nickname, incoming.Nickname)
	addScalar("Birthday", original.Birthday, incoming.Birthday)
	addScalar("Anniversary", original.Anniversary, incoming.Anniversary)
	addScalar("Gender", original.Gender, incoming.Gender)
	addScalar("Organization", original.Organization, incoming.Organization)
	addScalar("Department", original.Department, incoming.Department)
	addScalar("Job Title", original.JobTitle, incoming.JobTitle)
	addScalar("Role", original.Role, incoming.Role)
	addScalar("How We Met", original.HowWeMet, incoming.HowWeMet)
	addScalar("Work Information", original.WorkInformation, incoming.WorkInformation)
	addScalar("Contact Information", original.ContactInformation, incoming.ContactInformation)

	addAdded := func(label string, added []string) {
		if len(added) > 0 {
			changes = append(changes, fmt.Sprintf("- %s: added %s", label, strings.Join(added, ", ")))
		}
	}

	_, addedEmails := mergeContactValues(original.Emails, incoming.Emails, contactEmailBlank, contactEmailKey)
	addAdded("Emails", displayValues(addedEmails, contactEmailDisplay))

	_, addedPhones := mergeContactValues(original.Phones, incoming.Phones, contactPhoneBlank, contactPhoneKey)
	addAdded("Phones", displayValues(addedPhones, contactPhoneDisplay))

	_, addedAddresses := mergeContactValues(original.Addresses, incoming.Addresses, contactAddressBlank, contactAddressKey)
	addAdded("Addresses", displayValues(addedAddresses, contactAddressDisplay))

	_, addedURLs := mergeContactValues(original.URLs, incoming.URLs, contactURLBlank, contactURLKey)
	addAdded("URLs", displayValues(addedURLs, contactURLDisplay))

	_, addedIMPPs := mergeContactValues(original.IMPPs, incoming.IMPPs, contactIMPPBlank, contactIMPPKey)
	addAdded("IMPPs", displayValues(addedIMPPs, contactIMPPDisplay))

	if newCirclesStr := strings.Join(incoming.Circles, ", "); newCirclesStr != "" {
		oldCircles := strings.Join(original.Circles, ", ")
		if oldCircles != newCirclesStr {
			if oldCircles != "" {
				changes = append(changes, fmt.Sprintf("- Circles: %s → %s", oldCircles, newCirclesStr))
			} else {
				changes = append(changes, fmt.Sprintf("- Circles: (empty) → %s", newCirclesStr))
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	content := fmt.Sprintf("%s Import updated this contact.\n\nChanges made:\n%s", importType, strings.Join(changes, "\n"))

	note := models.Note{
		UserID:    userID,
		ContactID: &contactID,
		Content:   content,
	}

	return db.Create(&note).Error
}
