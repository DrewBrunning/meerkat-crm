// Matrix is the DATA-01 field compatibility matrix (issue #441): every
// canonical field classified per serialized format into the v0.6.5 milestone's
// five buckets (exact, transformed, extended, unsupported, lossy).
//
// It is GENERATED, never hand-authored: the primary source is the locked
// correspondence oracle (testdata/correspondence.tsv, ADR-0002), one matrix
// row per concept_id, and the secondary source is the issue #515 canonical
// field audit for fields with no correspondence row at all (Gender and its
// peers, which never enter a serialized file). Nothing here encodes a mapping
// absent from the table — buckets are derived from the table's own columns
// (property presence, transform) and notes, with the handful of adapter-level
// redirects the notes already cite kept as explicit, table-grounded overrides.
//
// The committed rendering lives at docs/data-01-field-compatibility-matrix.md
// (regenerate with cmd/gencompatmatrix / `make gen-compat-matrix`); the drift
// test (matrix_test.go) fails if this package and the committed copy diverge.
//
// LossReports() is the DATA-02 (issue #442) input: every unsupported/lossy
// cell across the three serialized formats, with the reason a runtime loss
// report must name. The correspondence test asserts the matrix's
// unsupported/lossy set and LossReports() agree exactly in both directions.
package correspondence

import (
	"fmt"
	"strings"
)

// Format is one serialization surface the matrix classifies against.
type Format string

const (
	FormatVCard4    Format = "vcard4"
	FormatVCard3    Format = "vcard3"
	FormatJSContact Format = "jscontact"
	// FormatCardDAV is not a fourth serialization: CardDAV is a carrier that
	// negotiates the vCard version per request (see the matrix doc's
	// CardDAV-on-the-wire section), so its cells repeat the vCard 4.0
	// classification (the default) and annotate the vCard 3.0 classification
	// where it differs. Unsupported/lossy loss reports (DATA-02) exist only
	// for the three actual serialized formats, never for this carrier.
	FormatCardDAV Format = "carddav"
)

// serializedFormats is the order in which the actual wire formats appear, used
// for loss-report ordering and for the "one report per unsupported/lossy cell"
// correspondence. CardDAV is deliberately excluded: it is a negotiation of the
// two vCard formats, not a format of its own.
var serializedFormats = []Format{FormatVCard4, FormatVCard3, FormatJSContact}

// formatName returns the human name of a Format for report reasons.
func formatName(f Format) string {
	switch f {
	case FormatVCard4:
		return "vCard 4.0"
	case FormatVCard3:
		return "vCard 3.0"
	case FormatJSContact:
		return "JSContact"
	case FormatCardDAV:
		return "CardDAV"
	}
	return string(f)
}

// Bucket is the milestone's five-way field classification.
type Bucket string

const (
	// BucketExact round-trips with no transform (transform == identity).
	BucketExact Bucket = "exact"
	// BucketTransformed round-trips through the table's declared value transform.
	BucketTransformed Bucket = "transformed"
	// BucketExtended is carried via an X- property or a JSContact extension /
	// passthrough escape hatch (issue #514).
	BucketExtended Bucket = "extended"
	// BucketUnsupported has no home in that format; drops with a warn
	// diagnostic per ADR-0002's degradation policy.
	BucketUnsupported Bucket = "unsupported"
	// BucketLossy survives but with reduced fidelity (precision, structure,
	// or cardinality).
	BucketLossy Bucket = "lossy"
)

// Cell is one canonical field's classification for one format.
type Cell struct {
	Bucket Bucket
	// Label is the vCard property (possibly with a structured-value index
	// like N[0]), JSContact pointer, or the "-" / "*verbatim*" sentinel.
	Label string
	// Reason explains an unsupported/lossy/extended classification. This is
	// the "why" a DATA-02 loss report must carry for this cell.
	Reason string
}

// Entry is one canonical field's classification across all four format
// columns. Source is "correspondence" for rows of the ADR-0002 table and
// "audit-515" for canonical fields with no correspondence row.
type Entry struct {
	ConceptID   string
	NeutralPath string
	Transform   string
	Notes       string
	Source      string
	NoHome      *NoHomeField // non-nil only for Source == "audit-515"
	Cells       map[Format]Cell
}

// NoHomeField is one canonical field from the issue #515 audit that has no
// correspondence-table row. LossReport distinguishes the envelope fields
// (which produce an EnvelopeExportLossDiagnostics warn and therefore need a
// DATA-02 loss report) from the deliberate policy exclusions (CRM-local flags
// and relational timeline tables, which are written-down decisions never to
// export and must not be reported as fidelity loss).
type NoHomeField struct {
	Key        string // matrix concept id (matches EnvelopeExportLossDiagnostics concepts where they exist)
	Field      string // the canonical field / envelope key
	Home       string // where it round-trips through the neutral Record
	Note       string
	LossReport bool
}

// noHomeFields is the fixed issue #515 audit output: canonical Contact fields
// with no correspondence row. Order here is the order they appear in the
// matrix document and in LossReports().
var noHomeFields = []NoHomeField{
	{
		Key: "crm.gender", Field: "Gender", Home: "CRMEnvelope.Gender", LossReport: true,
		Note: "free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary",
	},
	{
		Key: "crm.how_we_met", Field: "HowWeMet", Home: "CRMEnvelope.HowWeMet", LossReport: true,
		Note: "CRM-only envelope field; never serialized",
	},
	{
		Key: "crm.work_information", Field: "WorkInformation", Home: "CRMEnvelope.WorkInformation", LossReport: true,
		Note: "CRM-only envelope field; never serialized",
	},
	{
		Key: "crm.contact_information", Field: "ContactInformation", Home: "CRMEnvelope.ContactInformation", LossReport: true,
		Note: "CRM-only envelope field; never serialized",
	},
	{
		Key: "crm.circles", Field: "Circles", Home: "CRMEnvelope.Circles", LossReport: true,
		Note: "legacy column superseded as a data source by circle_members (T2/T3); never serialized",
	},
	{
		Key: "crm.archived", Field: "Archived", Home: "none (Contact.Archived flat column)", LossReport: false,
		Note: "CRM-local flag; deliberately never exported (ADR-0002 audit rule 4)",
	},
	{
		Key: "crm.is_favorite", Field: "IsFavorite", Home: "none (Contact.IsFavorite flat column)", LossReport: false,
		Note: "CRM-local flag; deliberately never exported (issue #173)",
	},
	{
		Key: "crm.notes", Field: "Notes (table)", Home: "none (relational timeline entity)", LossReport: false,
		Note: "relational table keyed by contact, like Activities/Reminders; deliberately not projected into Card.Notes",
	},
	{
		Key: "crm.activities", Field: "Activities (table)", Home: "none (relational timeline entity)", LossReport: false,
		Note: "separate relational entity, keyed by contact ID; never embedded in the single-contact Record",
	},
	{
		Key: "crm.reminders", Field: "Reminders (table)", Home: "none (relational timeline entity)", LossReport: false,
		Note: "separate relational entity, keyed by contact ID; never embedded in the single-contact Record",
	},
}

// v4LossyReason documents the vCard 4.0 cells that are supported but lossy,
// keyed by concept_id. Every entry is grounded in the correspondence table's
// own notes column.
var v4LossyReason = map[string]string{
	"anniversary.place.birth": "Address structure flattened to BIRTHPLACE TEXT/URI (RFC 6474 §2.1); structure lossy, warns",
	"anniversary.place.death": "Address structure flattened to DEATHPLACE TEXT/URI (RFC 6474 §2.2); structure lossy, warns",
}

// v3Special overrides the mechanical vCard 3.0 classification for the rows
// whose v3_prop is "-" but whose data is nevertheless carried by an
// adapter-level redirect the correspondence table's own notes cite, and for
// the passthrough escape hatch. Each entry names the redirect's target and the
// reason; none of them invents a mapping — every one traces to the row's notes
// or the adapter's documented degradation.
var v3Special = map[string]Cell{
	"adr.geo": {
		Bucket: BucketTransformed, Label: "GEO",
		Reason: "no ADR GEO param in v3; emitted as a separate GEO property (lat;lon)",
	},
	"adr.tz": {
		Bucket: BucketTransformed, Label: "TZ",
		Reason: "no ADR TZ param in v3; emitted as a separate TZ property",
	},
	"anniversary.wedding": {
		Bucket: BucketExtended, Label: "X-ANNIVERSARY",
		Reason: "v3 has no ANNIVERSARY property; adapter emits X-ANNIVERSARY with a warn",
	},
	"related": {
		Bucket: BucketLossy, Label: "AGENT",
		Reason: "v3 has no RELATED property; target redirected to AGENT with a warn, relation TYPE tokens lost",
	},
}

// v4Special overrides the mechanical vCard 4.0 classification for the rows
// whose v4_prop is a standards extension property rather than a native home.
var v4Special = map[string]Cell{
	"pt.jscontact": {
		Bucket: BucketExtended, Label: "JSPROP",
		Reason: "JSContact-only unknowns carried via the RFC 9555 JSPROP/JSPTR extension property",
	},
	"pt.vcard": {
		Bucket: BucketExtended, Label: "*verbatim*",
		Reason: "passthrough escape hatch: stored jCard props re-emitted verbatim",
	},
}

// v3LossyReason documents the vCard 3.0 cells that are supported (v3_prop
// present) but lossy, keyed by concept_id. Grounded in the table's notes /
// the adapter's own warn diagnostics.
var v3LossyReason = map[string]string{
	"adr":               "v3 ADR has only the 7 legacy fields; RFC 9553/9554 component kinds beyond those and the CC parameter are dropped with a warn",
	"anniversary.birth": "v3 BDAY is date-only; time-of-day dropped (warns)",
	"note":              "v3 NOTE drops the AUTHOR/AUTHOR-NAME/CREATED params (RFC 9554-only; warns)",
}

// jsSpecial overrides the mechanical JSContact classification for the two
// passthrough rows.
var jsSpecial = map[string]Cell{
	"pt.vcard": {
		Bucket: BucketExtended, Label: "/vCardProps",
		Reason: "carried via the JSContact vCardProps extension member (RFC 9555)",
	},
	"pt.jscontact": {
		Bucket: BucketExact, Label: "(pointer keys)",
		Reason: "re-spliced verbatim at the recorded JSON pointers",
	},
}

// jsLossyReason documents the JSContact cells that are supported but lossy,
// keyed by concept_id. Grounded in the adapter's own warn diagnostics.
var jsLossyReason = map[string]string{
	"gramgender": "JSContact speakToAs.grammaticalGender is scalar (RFC 9553 §2.2.4); multiple neutral entries collapse to the language-selected/first entry (warns)",
	"keywords":   "JSContact keywords is a boolean-set; duplicate keywords collapse (warns)",
}

// propLabel strips a structured-value index suffix ("N[0]" -> "N") for
// display, and passes the "*verbatim*" sentinel through unchanged.
func propLabel(prop string) string {
	if idx := strings.Index(prop, "["); idx >= 0 {
		return prop[:idx]
	}
	return prop
}

// unsupportedCell is the mechanical "no home" cell.
func unsupportedCell(f Format) Cell {
	return Cell{
		Bucket: BucketUnsupported, Label: "-",
		Reason: fmt.Sprintf("no %s home; dropped with a warn diagnostic (ADR-0002 degradation policy)", formatName(f)),
	}
}

// classifyV4 derives the vCard 4.0 cell from the row's own columns.
func classifyV4(r Row) Cell {
	if special, ok := v4Special[r.ConceptID]; ok {
		return special
	}
	label := propLabel(r.V4Prop)
	switch {
	case r.V4Prop == "-":
		return unsupportedCell(FormatVCard4)
	case strings.HasPrefix(r.V4Prop, "X-"):
		return Cell{Bucket: BucketExtended, Label: label, Reason: "X- property"}
	}
	if reason := v4LossyReason[r.ConceptID]; reason != "" {
		return Cell{Bucket: BucketLossy, Label: label, Reason: reason}
	}
	if r.Transform == "identity" {
		return Cell{Bucket: BucketExact, Label: label}
	}
	return Cell{Bucket: BucketTransformed, Label: label}
}

// classifyV3 derives the vCard 3.0 cell from the row's own columns plus the
// adapter-level redirects the notes cite (v3Special).
func classifyV3(r Row) Cell {
	if special, ok := v3Special[r.ConceptID]; ok {
		return special
	}
	label := propLabel(r.V3Prop)
	switch {
	case r.V3Prop == "-":
		return unsupportedCell(FormatVCard3)
	case r.V3Prop == "*verbatim*":
		return v4Special["pt.vcard"]
	case strings.HasPrefix(r.V3Prop, "X-"):
		return Cell{Bucket: BucketExtended, Label: label, Reason: "X- property"}
	}
	if reason := v3LossyReason[r.ConceptID]; reason != "" {
		return Cell{Bucket: BucketLossy, Label: label, Reason: reason}
	}
	if r.Transform == "identity" {
		return Cell{Bucket: BucketExact, Label: label}
	}
	return Cell{Bucket: BucketTransformed, Label: label}
}

// classifyJS derives the JSContact cell from the row's js_ptr column.
func classifyJS(r Row) Cell {
	if special, ok := jsSpecial[r.ConceptID]; ok {
		return special
	}
	label := r.JSPtr
	switch {
	case r.JSPtr == "-":
		return unsupportedCell(FormatJSContact)
	case strings.HasPrefix(r.JSPtr, "/"):
		// fallthrough: a pointer is a real home
	default:
		return Cell{Bucket: BucketExtended, Label: label, Reason: "JSContact extension / escape hatch"}
	}
	if reason := jsLossyReason[r.ConceptID]; reason != "" {
		return Cell{Bucket: BucketLossy, Label: label, Reason: reason}
	}
	if r.Transform == "identity" {
		return Cell{Bucket: BucketExact, Label: label}
	}
	return Cell{Bucket: BucketTransformed, Label: label}
}

// classifyCardDAV repeats the vCard 4.0 classification (the negotiation
// default) and annotates the vCard 3.0 classification where it differs.
func classifyCardDAV(v4, v3 Cell) Cell {
	c := v4
	if v3.Bucket != v4.Bucket {
		c.Reason = fmt.Sprintf("v3: %s", v3.Bucket)
	}
	return c
}

// Build returns the full matrix: one Entry per correspondence row (in table
// order) followed by one Entry per issue #515 no-home canonical field.
func Build() []Entry {
	var entries []Entry
	for _, r := range Load() {
		v4 := classifyV4(r)
		v3 := classifyV3(r)
		js := classifyJS(r)
		entries = append(entries, Entry{
			ConceptID:   r.ConceptID,
			NeutralPath: r.NeutralPath,
			Transform:   r.Transform,
			Notes:       r.Notes,
			Source:      "correspondence",
			Cells: map[Format]Cell{
				FormatVCard4:    v4,
				FormatVCard3:    v3,
				FormatJSContact: js,
				FormatCardDAV:   classifyCardDAV(v4, v3),
			},
		})
	}
	for _, nf := range noHomeFields {
		entries = append(entries, noHomeEntry(nf))
	}
	return entries
}

// noHomeEntry builds the all-formats-unsupported Entry for an issue #515 field.
func noHomeEntry(nf NoHomeField) Entry {
	reason := nf.Note
	cell := Cell{Bucket: BucketUnsupported, Label: "-", Reason: reason}
	return Entry{
		ConceptID:   nf.Key,
		NeutralPath: nf.Home,
		Transform:   "-",
		Notes:       nf.Note,
		Source:      "audit-515",
		NoHome:      &nf,
		Cells: map[Format]Cell{
			FormatVCard4:    cell,
			FormatVCard3:    cell,
			FormatJSContact: cell,
			FormatCardDAV:   cell,
		},
	}
}

// LossReport is one unsupported/lossy (field, format) pair plus the reason a
// DATA-02 runtime loss report must name. It is the input #442 consumes.
type LossReport struct {
	Concept string
	Format  Format
	Bucket  Bucket
	Reason  string
}

// LossReports returns the DATA-02 loss-report registry: exactly the matrix's
// unsupported/lossy cells across the three serialized formats, excluding the
// issue #515 policy exclusions (CRM-local flags and relational timeline
// tables, which are deliberate decisions never to export, not fidelity loss).
// The correspondence is asserted in both directions by matrix_test.go.
func LossReports() []LossReport {
	var out []LossReport
	for _, e := range Build() {
		if e.NoHome != nil && !e.NoHome.LossReport {
			continue
		}
		for _, f := range serializedFormats {
			c := e.Cells[f]
			if c.Bucket == BucketUnsupported || c.Bucket == BucketLossy {
				out = append(out, LossReport{
					Concept: e.ConceptID,
					Format:  f,
					Bucket:  c.Bucket,
					Reason:  c.Reason,
				})
			}
		}
	}
	return out
}

// lossReportIndex indexes LossReports() by "concept|format" so a DATA-02
// runtime diagnostic can be classified in O(1) instead of re-deriving the
// matrix per event. It is derived from LossReports() alone, so the
// reportable-set correspondence (matrix_test.go) is the single source of truth
// for both the registry and this index.
var lossReportIndex = func() map[string]LossReport {
	m := make(map[string]LossReport, len(LossReports()))
	for _, lr := range LossReports() {
		m[lr.Concept+"|"+string(lr.Format)] = lr
	}
	return m
}()

// ClassificationFor returns the DATA-01 classification for a (concept, format)
// pair, ok=false when the pair is not a reportable unsupported/lossy cell.
// This is the runtime half of the DATA-02 correspondence: a loss report is
// only emitted when its concept resolves to a matrix entry here, so the set of
// reports the runtime can produce is exactly the matrix's unsupported/lossy
// set.
func ClassificationFor(concept string, format Format) (LossReport, bool) {
	lr, ok := lossReportIndex[concept+"|"+string(format)]
	return lr, ok
}

// orderedFormats is the render order for the format columns.
var orderedFormats = []Format{FormatVCard4, FormatVCard3, FormatJSContact, FormatCardDAV}

// renderCell renders one format cell for the markdown table.
func renderCell(c Cell) string {
	s := "**" + string(c.Bucket) + "**"
	if c.Label != "" && c.Label != "-" {
		s += " · `" + c.Label + "`"
	}
	if c.Reason != "" {
		s += " — " + c.Reason
	}
	return s
}

// Render produces the committed matrix document. It is deterministic: Build()
// feeds it in fixed order and every cell derives from the table columns.
func Render() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: Field Compatibility Matrix\n")
	b.WriteString("nav_order: 15\n")
	b.WriteString("---\n\n")
	b.WriteString("# DATA-01 — Field compatibility matrix\n\n")
	b.WriteString("> **Generated artifact — do not hand-edit.** The source of truth is the locked\n")
	b.WriteString("> correspondence oracle (`backend/correspondence/testdata/correspondence.tsv`, ADR-0002)\n")
	b.WriteString("> plus the issue #515 canonical-field audit for fields with no correspondence row.\n")
	b.WriteString("> Regenerate with `cd backend && go run ./cmd/gencompatmatrix` (or `make gen-compat-matrix`);\n")
	b.WriteString("> the drift test `backend/correspondence/matrix_test.go` fails if this file and the\n")
	b.WriteString("> generator disagree.\n\n")
	b.WriteString("Every canonical field, classified per format into the v0.6.5 milestone's five buckets.\n")
	b.WriteString("No classification here encodes a mapping absent from the correspondence table\n")
	b.WriteString("(ADR-0002): buckets derive from the table's own columns and notes.\n\n")

	// Bucket legend.
	b.WriteString("## Bucket legend\n\n")
	b.WriteString("| Bucket | Meaning |\n")
	b.WriteString("|---|---|\n")
	b.WriteString("| **exact** | Round-trips with no transform (identity). |\n")
	b.WriteString("| **transformed** | Round-trips through the table's declared value transform. |\n")
	b.WriteString("| **extended** | Carried via an `X-` property or a JSContact extension / passthrough escape hatch (issue #514). |\n")
	b.WriteString("| **unsupported** | No home in that format; dropped with a warn diagnostic per ADR-0002. |\n")
	b.WriteString("| **lossy** | Survives but with reduced fidelity (precision, structure, or cardinality). |\n\n")

	// CardDAV negotiation.
	b.WriteString("## CardDAV-on-the-wire\n\n")
	b.WriteString("The CardDAV column is not a fourth format: the server advertises `text/vcard` 3.0 and\n")
	b.WriteString("4.0 (`backend/carddav/backend.go`) and negotiates per request via the HTTP `Accept`\n")
	b.WriteString("header (RFC 6352 §10.4.1), **defaulting to vCard 4.0** when the client sends no\n")
	b.WriteString("`version=`. Each CardDAV cell therefore repeats the vCard 4.0 classification and notes\n")
	b.WriteString("the vCard 3.0 classification where it differs. Unsupported/lossy *loss reports*\n")
	b.WriteString("(DATA-02, issue #442) exist per serialized format — vCard 4.0, vCard 3.0, JSContact —\n")
	b.WriteString("never for the CardDAV carrier itself.\n\n")

	// Matrix table.
	b.WriteString("## Matrix — correspondence concepts\n\n")
	b.WriteString("| Concept | Neutral path | Transform | vCard 4.0 | vCard 3.0 | JSContact | CardDAV (default v4) |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, e := range Build() {
		if e.Source != "correspondence" {
			continue
		}
		b.WriteString("| `" + e.ConceptID + "` | `" + e.NeutralPath + "` | `" + e.Transform + "`")
		for _, f := range orderedFormats {
			b.WriteString(" | " + renderCell(e.Cells[f]))
		}
		b.WriteString(" |\n")
	}

	// No-home canonical fields.
	b.WriteString("\n## Canonical fields with no neutral-model home (issue #515)\n\n")
	b.WriteString("These canonical `models.Contact` fields have no correspondence-table row: they\n")
	b.WriteString("round-trip through the neutral Record (envelope fields) or have no neutral home at\n")
	b.WriteString("all (CRM-local flags and relational timeline tables), and are therefore classified\n")
	b.WriteString("**unsupported** in every format. The envelope fields are a *named* loss on file\n")
	b.WriteString("export (`models.EnvelopeExportLossDiagnostics`); the flags and tables are deliberate\n")
	b.WriteString("policy exclusions (ADR-0002 audit rule 4) and produce no loss report.\n\n")
	b.WriteString("| Canonical field | Neutral home | vCard 4.0 | vCard 3.0 | JSContact | CardDAV (default v4) |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, e := range Build() {
		if e.Source != "audit-515" {
			continue
		}
		b.WriteString("| `" + e.ConceptID + "` | `" + e.NeutralPath + "`")
		for _, f := range orderedFormats {
			b.WriteString(" | " + renderCell(e.Cells[f]))
		}
		b.WriteString(" |\n")
	}

	// Loss reports.
	b.WriteString("\n## Loss reports (DATA-02 input)\n\n")
	b.WriteString("Exactly the matrix's **unsupported** and **lossy** cells across the three serialized\n")
	b.WriteString("formats, with the reason a DATA-02 runtime loss report must name. The correspondence\n")
	b.WriteString("is bidirectional and asserted by `matrix_test.go` (every unsupported/lossy cell has a\n")
	b.WriteString("report, and every report is an unsupported/lossy cell).\n\n")
	b.WriteString("| Concept | Format | Bucket | Reason |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, lr := range LossReports() {
		b.WriteString("| `" + lr.Concept + "` | " + formatName(lr.Format) + " | **" + string(lr.Bucket) + "** | " + lr.Reason + " |\n")
	}

	return b.String()
}
