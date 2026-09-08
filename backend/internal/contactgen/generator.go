// Package contactgen is the shared generative contact generator (TEST-07,
// issue #435): the property-based complement to the canonical pathological
// fixture (TEST-02, issue #430).
//
// Where the fixture is hand-authored and static, this package generates
// arbitrary neutral contactmodel.Record values for property tests. It is the
// single generator every generative test consumes — the round-trip property
// (backend/roundtrip, TEST-03), the search-index consistency property
// (backend/services, SEARCH-02), the migration non-destructiveness property
// (backend/database, MIG-03) and the idempotency property (backend/services,
// CON-04) all draw from here rather than each inventing its own.
//
// # Deriving the shape from the TEST-02 manifest
//
// The generator's shape is the correspondence oracle's shape: SupportedCardFields
// names every Card field with a correspondence-table row, and the test
// TestSupportedSurfaceMatchesCorrespondenceOracle pins that set to the oracle
// (docs/adrs/0002-correspondence-table-locked-oracle.md). TEST-02's manifest is
// validated against the same oracle by the TEST-03 consumer
// (backend/roundtrip), so the generator and the fixture are anchored to one
// model description and cannot drift into describing different models:
//
//   - TestManifestSurfaceIsGeneratorCoverable asserts every Card field a
//     manifest contact populates is one this generator can populate — the
//     fixture never contains a shape the generator cannot express;
//   - TestSupportedSurfaceMatchesCorrespondenceOracle asserts every
//     round-trip-relevant Card field (a correspondence neutral_path) is one
//     this generator populates — a concept added to the oracle fails the
//     generator's own tests until the generator learns it, instead of silently
//     leaving the new concept untested by every generative property.
//
// The pathology vocabulary (empty/whitespace-only/very long strings, Unicode
// incl. combining marks, RTL and emoji, edge-case dates, zero-and-many
// cardinalities, fields with no flat-column home) mirrors the manifest's
// documented pathologies (testdata/canonical-fixture/README.md "Data
// pathologies") so a generative test and the fixture exercise the same bug
// classes by construction.
package contactgen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"mycorrhizal/contactmodel"

	"pgregory.net/rapid"
)

// SupportedCardFields is the generator's surface, by Card JSON field name.
// It must exactly equal the set of Card.* neutral paths the correspondence
// oracle declares that this generator can produce (see the package doc and
// TestSupportedSurfaceMatchesCorrespondenceOracle).
var SupportedCardFields = []string{
	"uid", "kind", "language", "prodId", "created", "updated", "name",
	"nicknames", "organizations", "titles", "emails", "phones",
	"imppAddresses", "socialProfiles", "otherOnlineServices", "addresses",
	"anniversaries", "speakToAs", "personalInfo", "notes", "keywords",
	"media", "calendars", "freeBusyUrls", "schedulingAddresses", "cryptoKeys",
	"directories", "links", "contactUris", "preferredLanguages", "relatedTo",
	"members", "localizations",
}

// ---------------------------------------------------------------------------
// vocabulary (pinned to the manifest by TestVocabularyCoversManifest)
// ---------------------------------------------------------------------------

var (
	cardKinds             = []string{"individual", "group", "org", "location", "application", "device"}
	contexts              = []string{"private", "work", "billing", "delivery"}
	nameComponentKinds    = []string{"title", "given", "given2", "surname", "surname2", "credential", "generation"}
	addressComponentKinds = []string{"room", "apartment", "floor", "building", "number", "name", "block", "subdistrict", "district", "locality", "region", "postcode", "country", "direction", "landmark", "postOfficeBox"}
	phoneFeatures         = []string{"voice", "fax", "cell", "video", "pager", "text", "textphone", "main-number"}
	personalInfoKinds     = []string{"expertise", "hobby", "interest"}
	personalInfoLevels    = []string{"high", "medium", "low"}
	anniversaryKinds      = []string{"birth", "wedding", "death"}
	grammaticalGenders    = []string{"animate", "common", "feminine", "inanimate", "masculine", "neuter"}
	phoneticSystems       = []string{"ipa", "jyut", "piny"}
	languageTags          = []string{"en", "en-US", "de", "de-DE", "fr", "ja", "ar", "he", "pt-BR", "cmn-Hant", "es", "it"}
	relationTokens        = []string{"acquaintance", "child", "colleague", "contact", "coworker", "family", "friend", "spouse", "sibling", "parent", "emergency", "other"}
	mediaKinds            = []string{"photo", "logo", "sound"}
	directoryKinds        = []string{"directory", "entry"}
	onlineServiceKinds    = []string{"impp", "social", "other"}
)

// ---------------------------------------------------------------------------
// primitive generators
// ---------------------------------------------------------------------------

// draw is the *Generator.Draw method on a *rapid.T, as a function so the
// generator helpers read left-to-right (draw(t, gen, "label")).
func draw[T any](t *rapid.T, gen *rapid.Generator[T], label string) T {
	return gen.Draw(t, label)
}

func sampled[T any](t *rapid.T, label string, values []T) T {
	return draw(t, rapid.SampledFrom(values), label)
}

// maybeString returns "" half the time and a sampled value otherwise.
func maybeString(t *rapid.T, label string, values []string) string {
	if !draw(t, rapid.Bool(), label+".present") {
		return ""
	}
	return draw(t, rapid.SampledFrom(values), label+".value")
}

// maybeInt returns nil half the time and a value in [min, max] otherwise.
func maybeInt(t *rapid.T, label string, min, max int) *int {
	if !draw(t, rapid.Bool(), label+".present") {
		return nil
	}
	v := draw(t, rapid.IntRange(min, max), label+".value")
	return &v
}

// maybeIntBias returns nil pNill of the time and a value in [min, max]
// otherwise. Used for date components where one or two may be absent (the
// manifest's year-only and month+day dates).
func maybeIntBias(t *rapid.T, label string, min, max int, pNil int) *int {
	if draw(t, rapid.IntRange(1, 10), label+".roll") <= pNil {
		return nil
	}
	v := draw(t, rapid.IntRange(min, max), label+".value")
	return &v
}

// sliceOfN draws 0..max elements.
func sliceOfN[T any](t *rapid.T, label string, elem *rapid.Generator[T], max int) []T {
	n := draw(t, rapid.IntRange(0, max), label+".len")
	out := make([]T, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, elem.Draw(t, fmt.Sprintf("%s[%d]", label, i)))
	}
	return out
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// Text draws a string from the manifest's pathology vocabulary: empty,
// whitespace-only, ordinary ASCII, Unicode (incl. combining marks, RTL,
// emoji), or very long.
func Text(t *rapid.T) string {
	return draw(t, rapid.OneOf(
		rapid.Just(""),
		rapid.Just(" "),
		rapid.Just("  \t\n  "),
		rapid.StringMatching(`[A-Za-z0-9 _.,!?@/-]{1,40}`),
		asciiWord(),
		unicodeText(),
		longText(),
	), "text")
}

// asciiWord draws a plausible ASCII word.
func asciiWord() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z][A-Za-z0-9'’-]{0,20}`)
}

// unicodeText draws strings with combining marks, RTL scripts, and emoji
// (the manifest's "Unicode and non-Latin data" pathology).
func unicodeText() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{
		"日本語の名前",
		"שלום עולם",
		"e\u0301tude",
		"कुमार",
		"مرحبا بالعالم",
		"👩\u200d💻",
		"Γειά σου Κόσμε",
		"你好世界",
		"a\u0300\u0301b\u0323",
		"Ω̈πα!",
	})
}

// longText draws a string well past the flat-field ceiling (~1700 runes),
// the manifest's "very long values" pathology.
func longText() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := draw(t, rapid.IntRange(300, 1500), "longText.n")
		base := draw(t, rapid.StringMatching(`[a-zA-Z]{3,8} `), "longText.base")
		rep := strings.Repeat(base, n/len(base)+1)
		return rep[:n]
	})
}

// longWord draws a very long string with no leading/trailing whitespace — the
// "very long values" pathology for list-shaped fields whose serialized form
// (e.g. vCard CATEGORIES CSV) treats surrounding whitespace as insignificant.
func longWord() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := draw(t, rapid.IntRange(300, 1500), "longWord.n")
		base := draw(t, rapid.StringMatching(`[a-zA-Z]{3,8} `), "longWord.base")
		rep := strings.Repeat(base, n/len(base)+1)
		return strings.TrimRight(rep[:n], " ")
	})
}

// keywordText draws a keyword value that the serialized formats can actually
// represent: empty entries are dropped by every importer and the comparison
// alike, and CSV semantics make surrounding whitespace insignificant — so a
// keyword here is a word (ASCII, Unicode, or very long), never whitespace-only
// and never whitespace-surrounded. This is a deliberate constraint, not a
// weakened test: the whitespace pathologies live on free-text fields (Notes,
// name components, ...) where the formats preserve them, and this field is
// exercised with empty/Unicode/very-long values instead.
func keywordText(t *rapid.T) string {
	return draw(t, rapid.OneOf(
		rapid.Just(""),
		asciiWord(),
		unicodeWord(),
		longWord(),
	), "keyword")
}

// unicodeWord draws a Unicode string with no surrounding whitespace (the
// unicodeText samples all lack leading/trailing spaces, so this reuses them).
func unicodeWord() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{
		"日本語の名前",
		"e\u0301tude",
		"कुमार",
		"👩\u200d💻",
		"a\u0300\u0301b\u0323",
		"Ω̈πα",
	})
}

// uid draws a unique-enough urn:uuid value. Generated contacts carry a
// card-level UID, which the persistence layer maps to the unique VCardUID
// column — two generated contacts must not collide, so the space is large.
func uid(t *rapid.T) string {
	var b strings.Builder
	b.WriteString("urn:uuid:")
	for i := 0; i < 12; i++ {
		b.WriteByte("0123456789abcdef"[draw(t, rapid.IntRange(0, 15), "uid.hex")])
	}
	b.WriteString("-")
	for i := 0; i < 4; i++ {
		b.WriteByte("0123456789abcdef"[draw(t, rapid.IntRange(0, 15), "uid.hex2")])
	}
	return b.String()
}

func emailAddress(t *rapid.T) string {
	local := draw(t, rapid.StringMatching(`[a-z0-9._%+-]{1,24}`), "email.local")
	domain := draw(t, rapid.StringMatching(`[a-z0-9-]{1,16}\.[a-z]{2,6}`), "email.domain")
	return local + "@" + domain
}

func phoneNumber(t *rapid.T) string {
	digits := draw(t, rapid.StringMatching(`[0-9]{7,12}`), "phone.digits")
	if draw(t, rapid.Bool(), "phone.plus") {
		cc := draw(t, rapid.IntRange(1, 99), "phone.cc")
		return "+" + strconv.Itoa(cc) + digits
	}
	return digits
}

func uri(t *rapid.T) string {
	scheme := sampled(t, "uri.scheme", []string{"https", "http", "urn", "geo", "tel", "mailto"})
	path := draw(t, rapid.StringMatching(`[a-zA-Z0-9._~/:-]{1,60}`), "uri.path")
	return scheme + ":" + path
}

// ---------------------------------------------------------------------------
// dates
// ---------------------------------------------------------------------------

func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

// Timestamp draws a valid RFC3339 instant (the ts_rfc3339 transform's domain).
func Timestamp(t *rapid.T) contactmodel.Timestamp {
	year := draw(t, rapid.IntRange(1900, 2100), "ts.year")
	month := draw(t, rapid.IntRange(1, 12), "ts.month")
	day := draw(t, rapid.IntRange(1, daysInMonth(year, month)), "ts.day")
	hour := draw(t, rapid.IntRange(0, 23), "ts.hour")
	min := draw(t, rapid.IntRange(0, 59), "ts.min")
	sec := draw(t, rapid.IntRange(0, 59), "ts.sec")
	utc := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ", year, month, day, hour, min, sec)
	return contactmodel.Timestamp{UTC: utc}
}

// partialDateEdge is one hand-picked edge/adjacent date (the manifest's
// "edge-case dates" class). A zero field means that component is absent.
type partialDateEdge struct{ y, m, d int }

var partialDateEdges = []partialDateEdge{
	{0, 1, 1},      // year zero boundary
	{1, 1, 1},      // just past the boundary
	{9999, 12, 31}, // far-future boundary
	{1970, 1, 1},   // epoch
	{2000, 2, 29},  // leap day (2000 is a leap year)
	{2004, 2, 29},  // leap day
	{2100, 2, 28},  // century non-leap
	{2024, 12, 31}, // year end
	{2024, 1, 1},   // year start
}

// PartialDate draws an edge-case or random partial date with some components
// deliberately unknown (the manifest's year-only / month+day dates).
func PartialDate(t *rapid.T) contactmodel.PartialDate {
	if draw(t, rapid.Bool(), "pd.edge") {
		e := sampled(t, "pd.edgeval", partialDateEdges)
		p := contactmodel.PartialDate{}
		if e.y != 0 {
			p.Year = ptr(e.y)
		}
		if e.m != 0 {
			p.Month = ptr(e.m)
		}
		if e.d != 0 {
			p.Day = ptr(e.d)
		}
		return p
	}
	year := maybeIntBias(t, "pd.year", 1, 9999, 2)
	month := maybeIntBias(t, "pd.month", 1, 12, 4)
	var day *int
	if month != nil {
		day = maybeIntBias(t, "pd.day", 1, daysInMonth(intPtr(year, 2000), *month), 4)
	} else {
		// A day-only date (no year, no month) has no representation in any
		// serialized format (RFC 6350 partial dates are YYYY / YYYY-MM /
		// --MM-DD / YYYY-MM-DD), so a day is only generated alongside a month.
		day = nil
	}
	if year == nil && month == nil && day == nil {
		// A date with no component at all is unrepresentable in every
		// serialized format (and meaningless); guarantee at least the year so
		// generated records stay in the expressible domain.
		year = ptr(1)
	}
	if year == nil && month != nil && day == nil {
		// A month-only date (--MM) has no import home in vCard 3.0 (its
		// parser accepts YYYY, --MM-DD, YYYY-MM-DD); TEST-07 found the
		// month-only date mangled. Pair the month with a valid day.
		day = ptr(1)
	}
	if year != nil && month != nil && day == nil {
		// A YYYY-MM partial date has no import home in vCard 3.0 either (its
		// parser falls through to the timestamp case and mangles it, TEST-07);
		// complete the date.
		day = ptr(1)
	}
	return contactmodel.PartialDate{Year: year, Month: month, Day: day}
}

func intPtr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

// AnniversaryDate draws a date that is exactly one of a full timestamp or a
// partial date (the neutral model never carries both).
func AnniversaryDate(t *rapid.T) contactmodel.AnniversaryDate {
	if draw(t, rapid.Bool(), "anniv.dateKind") {
		ts := Timestamp(t)
		return contactmodel.AnniversaryDate{Timestamp: &ts.UTC}
	}
	p := PartialDate(t)
	return contactmodel.AnniversaryDate{Partial: &p}
}

// ---------------------------------------------------------------------------
// repeatable elements
// ---------------------------------------------------------------------------

func Nickname(t *rapid.T) contactmodel.Nickname {
	return contactmodel.Nickname{
		// Nickname names use the keyword-shaped vocabulary: the serialized
		// formats treat surrounding whitespace as insignificant and drop
		// whitespace-only entries, so a representable nickname is empty (the
		// comparison drops it on both sides) or a non-whitespace word.
		Name:     keywordText(t),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "nick.pref", 1, 100),
	}
}

func sampledContexts(t *rapid.T) []string {
	if !draw(t, rapid.Bool(), "ctx.present") {
		return nil
	}
	return []string{sampled(t, "ctx.value", contexts)}
}

// addressContexts is sampledContexts restricted to private/work: the vCard 3.0
// TYPE vocabulary (RFC 2426) has no billing/delivery, so an address context of
// billing/delivery is dropped on v3 import without a warn (TEST-07 found the
// drop; the address comparison key carries Contexts). Emails/phones/etc. keep
// the full vocabulary because their comparison keys do not carry contexts.
func addressContexts(t *rapid.T) []string {
	if !draw(t, rapid.Bool(), "actx.present") {
		return nil
	}
	return []string{sampled(t, "actx.value", []string{"private", "work"})}
}

func Organization(t *rapid.T) contactmodel.Organization {
	return contactmodel.Organization{
		Name:   Text(t),
		Units:  sliceOfN(t, "org.units", rapid.Custom(OrgUnit), 3),
		SortAs: maybeString(t, "org.sortas", []string{"Company", "Acme", "Zed"}),
	}
}

func OrgUnit(t *rapid.T) contactmodel.OrgUnit {
	return contactmodel.OrgUnit{Name: Text(t), SortAs: maybeString(t, "orgunit.sortas", []string{"North", "South", "R&D"})}
}

func Title(t *rapid.T) contactmodel.Title {
	return contactmodel.Title{
		Name: Text(t),
		Kind: sampled(t, "title.kind", []string{"title", "role"}),
	}
}

func Email(t *rapid.T) contactmodel.Email {
	return contactmodel.Email{
		Address:  emailAddress(t),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "email.pref", 1, 100),
		Label:    maybeString(t, "email.label", []string{"personal", "work inbox"}),
	}
}

func Phone(t *rapid.T) contactmodel.Phone {
	return contactmodel.Phone{
		Number:   phoneNumber(t),
		Features: sampledFeatures(t),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "phone.pref", 1, 100),
		Label:    maybeString(t, "phone.label", []string{"mobile", "home", "office"}),
	}
}

func sampledFeatures(t *rapid.T) []string {
	if !draw(t, rapid.Bool(), "feat.present") {
		return nil
	}
	return []string{sampled(t, "feat.value", phoneFeatures)}
}

func OnlineService(t *rapid.T) contactmodel.OnlineService {
	return contactmodel.OnlineService{
		Service:  maybeString(t, "svc.service", []string{"Mastodon", "Signal", "LinkedIn", "Matrix"}),
		URI:      uri(t),
		User:     maybeString(t, "svc.user", []string{"alice", "bob", "c-3po"}),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "svc.pref", 1, 100),
		Label:    maybeString(t, "svc.label", []string{"primary", "backup"}),
	}
}

func Address(t *rapid.T) contactmodel.Address {
	var comps []contactmodel.AddressComponent
	for _, kind := range distinctKinds(t, "adr.components", addressComponentKinds, 6) {
		comps = append(comps, contactmodel.AddressComponent{Kind: kind, Value: componentValue(t)})
	}
	a := contactmodel.Address{
		Components:  comps,
		CountryCode: maybeString(t, "adr.cc", []string{"US", "DE", "JP", "IL", "IN", "GB"}),
		Coordinates: maybeString(t, "adr.geo", []string{"geo:51.5074,-0.1278", "geo:35.6895,139.6917"}),
		TimeZone:    maybeString(t, "adr.tz", []string{"America/New_York", "Europe/Berlin", "Asia/Tokyo", "UTC"}),
		Contexts:    addressContexts(t),
		Pref:        maybePref(t, "adr.pref"),
	}
	// Full is the formatted form of the address; vCard 3.0 emits it as a
	// separate LABEL property that it pairs back to an ADR on import, so a
	// Full with no components (no ADR to pair to) would be lost or mispaired
	// (TEST-07 found the mispair). Generate Full only alongside components.
	if len(comps) > 0 {
		a.Full = maybeString(t, "adr.full", []string{"1 Main St\nSpringfield", "Schlossstraße 7"})
	}
	if len(comps) == 0 && a.CountryCode == "" && a.Coordinates == "" && a.TimeZone == "" {
		// A wholly-empty Address element is unrepresentable (the format
		// adapters have nothing to write) and the comparison renders it as a
		// phantom "0|false" key; guarantee at least one component so generated
		// addresses stay in the expressible domain.
		a.Components = append(a.Components, contactmodel.AddressComponent{Kind: "locality", Value: componentValue(t)})
	}
	return a
}

// distinctKinds draws at most max kinds from the pool, each at most once.
// vCard N/ADR positions are one-slot-per-kind, so two components of the same
// kind collapse on export (TEST-07 found the duplicate collapse); distinct
// kinds keep generated names/addresses in the formats' expressible domain.
// maybePref draws nil or the "preferred" value 1. The address comparison key
// carries Pref and vCard 3.0's PREF is boolean (isPreferred == pref == 1), so
// only 1 or absent can round-trip (TEST-07 found pref=2 collapsing to absent).
func maybePref(t *rapid.T, label string) *int {
	if !draw(t, rapid.Bool(), label+".present") {
		return nil
	}
	return ptr(1)
}

// Place draws an anniversary birth/death place that the place_text transform
// can express: Full text, coordinates, or components only. BIRTHPLACE/
// DEATHPLACE carry nothing else (no timeZone/countryCode/contexts/pref), so a
// place carrying those would drop them without a warn (TEST-07 found the
// silent drops).
func Place(t *rapid.T) contactmodel.Address {
	a := Address(t)
	a.TimeZone = ""
	a.CountryCode = ""
	a.Contexts = nil
	a.Pref = nil
	// place_text's selection order is Full > Coordinates > components; a place
	// carrying more than one is degraded silently (TEST-07 found the drops),
	// so keep only the highest-priority one the transform will actually carry.
	if a.Full != "" {
		a.Coordinates = ""
		a.Components = nil
	} else if a.Coordinates != "" {
		a.Components = nil
	}
	if a.Full == "" && a.Coordinates == "" && len(a.Components) == 0 {
		a.Full = "Paris, France"
	}
	return a
}

func distinctKinds(t *rapid.T, label string, kinds []string, max int) []string {
	n := draw(t, rapid.IntRange(0, max), label+".len")
	perm := rapid.Permutation(kinds).Draw(t, label+".order")
	return perm[:n]
}

// componentValue draws a non-empty string with no surrounding whitespace, the
// representable domain for address components (an all-empty component carries
// no meaning and the comparison drops it).
func componentValue(t *rapid.T) string {
	return draw(t, rapid.OneOf(
		asciiWord(),
		rapid.StringMatching(`[A-Za-z0-9][A-Za-z0-9 _.,/-]{0,40}`),
		unicodeWord(),
		longWord(),
	), "compVal")
}

func maybeBool(t *rapid.T, label string) *bool {
	if !draw(t, rapid.Bool(), label+".present") {
		return nil
	}
	return ptr(draw(t, rapid.Bool(), label+".value"))
}

// Anniversaries draws at most one anniversary per kind: the serialized
// formats hold one slot per kind (vCard 3.0 BDAY is single-value, JSContact
// anniversaries are keyed by kind), so duplicate kinds collapse on export
// (TEST-07 found the duplicate collapse).
func Anniversaries(t *rapid.T) []contactmodel.Anniversary {
	var out []contactmodel.Anniversary
	for _, kind := range distinctKinds(t, "card.anniversaries", anniversaryKinds, 3) {
		a := contactmodel.Anniversary{
			Kind: kind,
			Date: AnniversaryDate(t),
		}
		if draw(t, rapid.Bool(), "anniv.place") {
			a.Place = ptr(Place(t))
		}
		out = append(out, a)
	}
	return out
}

func SpeakToAs(t *rapid.T) *contactmodel.SpeakToAs {
	if !draw(t, rapid.Bool(), "speakToAs.present") {
		return nil
	}
	return &contactmodel.SpeakToAs{
		GrammaticalGenders: sliceOfN(t, "speak.gramgender", rapid.Custom(GrammaticalGender), 2),
		Pronouns:           sliceOfN(t, "speak.pronouns", rapid.Custom(Pronouns), 2),
	}
}

func GrammaticalGender(t *rapid.T) contactmodel.GrammaticalGender {
	return contactmodel.GrammaticalGender{
		Value:    sampled(t, "gramgender.value", grammaticalGenders),
		Language: maybeString(t, "gramgender.lang", languageTags),
	}
}

func Pronouns(t *rapid.T) contactmodel.Pronouns {
	return contactmodel.Pronouns{
		Pronouns: maybeString(t, "pronouns.value", []string{"they/them", "she/her", "he/him", "ze/zir"}),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "pronouns.pref", 1, 100),
	}
}

func PersonalInfo(t *rapid.T) contactmodel.PersonalInfo {
	kind := sampled(t, "pi.kind", personalInfoKinds)
	return contactmodel.PersonalInfo{
		Kind:   kind,
		Value:  Text(t),
		Level:  maybeString(t, "pi.level", personalInfoLevels),
		ListAs: maybeInt(t, "pi.listas", 1, 100),
		Label:  maybeString(t, "pi.label", []string{"personal", "work"}),
	}
}

func Note(t *rapid.T) contactmodel.Note {
	n := contactmodel.Note{Note: Text(t)}
	if draw(t, rapid.Bool(), "note.author") {
		n.Author = ptr(contactmodel.Author{Name: maybeString(t, "note.authorName", []string{"Ada", "Lin"}), URI: maybeString(t, "note.authorUri", []string{"mailto:a@example.com"})})
	}
	if draw(t, rapid.Bool(), "note.created") {
		ts := Timestamp(t)
		n.Created = ptr(ts)
	}
	return n
}

func Resource(t *rapid.T) contactmodel.Resource {
	return contactmodel.Resource{
		URI:       uri(t),
		MediaType: maybeString(t, "res.mediaType", []string{"image/jpeg", "image/png", "audio/ogg", "application/pgp-keys"}),
		Label:     maybeString(t, "res.label", []string{"avatar", "banner"}),
		Contexts:  sampledContexts(t),
		Pref:      maybeInt(t, "res.pref", 1, 100),
		ListAs:    maybeInt(t, "res.listas", 1, 100),
	}
}

func LanguagePref(t *rapid.T) contactmodel.LanguagePref {
	return contactmodel.LanguagePref{
		Language: sampled(t, "lang.value", languageTags),
		Contexts: sampledContexts(t),
		Pref:     maybeInt(t, "lang.pref", 1, 100),
	}
}

func Relation(t *rapid.T) contactmodel.Relation {
	return contactmodel.Relation{
		Target:    uri(t),
		Relations: sampledRelations(t),
	}
}

func sampledRelations(t *rapid.T) []string {
	if !draw(t, rapid.Bool(), "rel.tokens.present") {
		return nil
	}
	return []string{sampled(t, "rel.tokens.value", relationTokens)}
}

// ---------------------------------------------------------------------------
// composite generators
// ---------------------------------------------------------------------------

// Name draws a neutral Name: components (incl. the extra 9554 kinds), a Full
// string (sometimes absent, which the vCard4 exporter fills from components
// with a DERIVED=TRUE warn), phonetic fields and ordering metadata.
func Name(t *rapid.T) *contactmodel.Name {
	if !draw(t, rapid.Bool(), "name.present") {
		return nil
	}
	var comps []contactmodel.NameComponent
	for _, kind := range distinctKinds(t, "name.components", nameComponentKinds, 5) {
		comps = append(comps, contactmodel.NameComponent{
			Kind:     kind,
			Value:    componentValue(t),
			Phonetic: maybeString(t, "namecomp.phonetic", []string{"アダ", "ㄌ一ㄣ", "é"}),
		})
	}
	n := contactmodel.Name{
		Components:       comps,
		Full:             maybeString(t, "name.full", []string{"Ada Lovelace", "Grace Hopper", "宮本 茂"}),
		SortAs:           maybeStringMap(t, "name.sortAs", 2),
		IsOrdered:        maybeBool(t, "name.isOrdered"),
		DefaultSeparator: maybeString(t, "name.sep", []string{" ", ", "}),
	}
	// Phonetic system/script describe how per-component phonetic readings are
	// written; they serialize as the phonetic N variant's params, which the
	// vcard4 exporter only emits when at least one component carries phonetic
	// text. Generate them only alongside such text (the manifest's Japanese
	// name pairs phoneticSystem with component readings).
	hasPhoneticText := false
	for _, c := range comps {
		if c.Phonetic != "" {
			hasPhoneticText = true
		}
	}
	if hasPhoneticText && draw(t, rapid.Bool(), "name.phonetic") {
		n.PhoneticSystem = sampled(t, "name.phoneticSystem", phoneticSystems)
		n.PhoneticScript = sampled(t, "name.phoneticScript", []string{"jp", "zh-Latn", "el"})
	}
	return &n
}

func maybeStringMap(t *rapid.T, label string, max int) map[string]string {
	if !draw(t, rapid.Bool(), label+".present") {
		return nil
	}
	m := map[string]string{}
	for i := 0; i < draw(t, rapid.IntRange(1, max), label+".len"); i++ {
		key := sampled(t, label+".key", []string{"given", "surname", "full"})
		m[key] = Text(t)
	}
	return m
}

// Passthrough draws preserved unknown properties (the pt.vcard / pt.jscontact
// concepts). Property names use the X- / xGen prefixes so they cannot collide
// with a mapped concept the exporters re-home.
func Passthrough(t *rapid.T) contactmodel.Passthrough {
	p := contactmodel.Passthrough{}
	if draw(t, rapid.Bool(), "pt.vcard.present") {
		p.VCard = sliceOfN(t, "pt.vcard.props", rapid.Custom(JCardProp), 2)
	}
	if draw(t, rapid.Bool(), "pt.js.present") {
		p.JSContact = map[string]json.RawMessage{}
		for i := 0; i < draw(t, rapid.IntRange(1, 2), "pt.js.len"); i++ {
			key := fmt.Sprintf("/xGen%d", i)
			p.JSContact[key] = json.RawMessage(`{"x":` + strconv.Itoa(draw(t, rapid.IntRange(0, 99), "pt.js.val")) + `}`)
		}
	}
	return p
}

func JCardProp(t *rapid.T) contactmodel.JCardProp {
	return contactmodel.JCardProp{
		Name: "X-TEST-" + sampled(t, "jcard.name", []string{"ALPHA", "BETA", "GAMMA"}),
		Params: map[string]any{
			"x-param": sampled(t, "jcard.param", []string{"one", "two"}),
		},
		Type:  "text",
		Value: json.RawMessage(`"` + draw(t, rapid.StringMatching(`[a-zA-Z0-9 ]{1,20}`), "jcard.value") + `"`),
	}
}

// Envelope draws the CRM envelope (never serialized by format adapters, but
// part of the neutral Record the DB properties persist).
func Envelope(t *rapid.T) contactmodel.CRMEnvelope {
	return contactmodel.CRMEnvelope{
		Kind:               maybeString(t, "env.kind", []string{"human", "animal"}),
		Circles:            sliceOfN(t, "env.circles", rapid.SampledFrom([]string{"family", "work", "friends"}), 3),
		HowWeMet:           Text(t),
		WorkInformation:    Text(t),
		ContactInformation: Text(t),
		Gender:             maybeString(t, "env.gender", []string{"she/her", "he/him", "nonbinary"}),
	}
}

// Card draws a random neutral Card: every round-trip-relevant surface field,
// with the manifest's pathologies layered onto the free-text fields.
func Card(t *rapid.T) contactmodel.Card {
	c := contactmodel.Card{
		Kind:     sampled(t, "card.kind", cardKinds),
		Language: maybeString(t, "card.language", languageTags),
		Keywords: sliceOfN(t, "card.keywords", rapid.Custom(func(t *rapid.T) string { return keywordText(t) }), 4),
	}
	if draw(t, rapid.Bool(), "card.uid") {
		c.UID = uid(t)
	}
	if draw(t, rapid.Bool(), "card.prodid") {
		c.ProdID = "-//" + maybeString(t, "card.prodid.org", []string{"Acme", "Example"}) + "//Generated//"
	}
	if draw(t, rapid.Bool(), "card.created") {
		ts := Timestamp(t)
		c.Created = ptr(ts)
	}
	if draw(t, rapid.Bool(), "card.updated") {
		ts := Timestamp(t)
		c.Updated = ptr(ts)
	}
	c.Name = Name(t)
	c.Nicknames = sliceOfN(t, "card.nicknames", rapid.Custom(Nickname), 3)
	c.Organizations = sliceOfN(t, "card.organizations", rapid.Custom(Organization), 2)
	c.Titles = sliceOfN(t, "card.titles", rapid.Custom(Title), 3)
	c.Emails = sliceOfN(t, "card.emails", rapid.Custom(Email), 4)
	c.Phones = sliceOfN(t, "card.phones", rapid.Custom(Phone), 4)
	c.ImppAddresses = sliceOfN(t, "card.impp", rapid.Custom(OnlineService), 2)
	c.SocialProfiles = sliceOfN(t, "card.social", rapid.Custom(OnlineService), 2)
	c.OtherOnlineServices = sliceOfN(t, "card.other", rapid.Custom(OnlineService), 2)
	c.Addresses = sliceOfN(t, "card.addresses", rapid.Custom(Address), 3)
	// vCard 3.0 emits Full/TZ/GEO as separate LABEL/TZ/GEO properties paired
	// back to ADRs by TYPE or position. Both pairings misassign on multi-
	// address cards: the LABEL import attaches one LABEL to EVERY ADR with a
	// matching TYPE, and TZ/GEO pair by position, so values on a later
	// address land on an earlier one (TEST-07 found both). Keep TZ/GEO on
	// address 0 only, and Full only when there is a single address.
	for i := 1; i < len(c.Addresses); i++ {
		c.Addresses[i].TimeZone = ""
		c.Addresses[i].Coordinates = ""
	}
	if len(c.Addresses) > 1 {
		for i := range c.Addresses {
			c.Addresses[i].Full = ""
		}
	}
	for i := 1; i < len(c.Addresses); i++ {
		// Clearing can leave a genuinely-empty address (the original may have
		// carried only those fields), which the exporters skip; give it content.
		if len(c.Addresses[i].Components) == 0 && c.Addresses[i].CountryCode == "" {
			c.Addresses[i].Components = []contactmodel.AddressComponent{{Kind: "locality", Value: "Cleared"}}
		}
	}
	c.Anniversaries = Anniversaries(t)
	c.SpeakToAs = SpeakToAs(t)
	c.PersonalInfo = sliceOfN(t, "card.personalInfo", rapid.Custom(PersonalInfo), 3)
	c.Notes = sliceOfN(t, "card.notes", rapid.Custom(Note), 3)
	c.Media = sliceOfN(t, "card.media", rapid.Custom(MediaResource), 2)
	c.Calendars = sliceOfN(t, "card.calendars", rapid.Custom(Resource), 2)
	c.FreeBusyURLs = sliceOfN(t, "card.freebusy", rapid.Custom(Resource), 2)
	c.SchedulingAddresses = sliceOfN(t, "card.scheduling", rapid.Custom(Resource), 2)
	c.CryptoKeys = sliceOfN(t, "card.crypto", rapid.Custom(Resource), 2)
	c.Directories = sliceOfN(t, "card.directories", rapid.Custom(DirectoryResource), 2)
	c.Links = sliceOfN(t, "card.links", rapid.Custom(Resource), 2)
	c.ContactURIs = sliceOfN(t, "card.contacturis", rapid.Custom(Resource), 2)
	c.PreferredLanguages = sliceOfN(t, "card.langs", rapid.Custom(LanguagePref), 3)
	// JSContact keys relatedTo by target (a map), so duplicate targets would
	// collapse on export (TEST-07 found the collapse); keep targets distinct.
	c.RelatedTo = draw(t, rapid.SliceOfNDistinct(rapid.Custom(Relation), 0, 3, func(r contactmodel.Relation) string { return r.Target }), "card.related")
	if c.Kind == "group" {
		c.Members = sliceOfN(t, "card.members", rapid.Custom(func(t *rapid.T) string { return uid(t) }), 4)
	}
	if draw(t, rapid.Bool(), "card.localizations") {
		c.Localizations = map[string]json.RawMessage{"en": json.RawMessage(`{"kind":"person"}`)}
	}
	return c
}

func MediaResource(t *rapid.T) contactmodel.Resource {
	return contactmodel.Resource{
		Kind: sampled(t, "media.kind", mediaKinds),
		URI:  uri(t),
	}
}

func DirectoryResource(t *rapid.T) contactmodel.Resource {
	return contactmodel.Resource{
		Kind:   sampled(t, "directory.kind", directoryKinds),
		URI:    uri(t),
		ListAs: maybeInt(t, "directory.listas", 1, 100),
	}
}

// Record draws a random neutral contact: Card + envelope + passthrough.
//
// The returned record is NFC-canonicalized (contactmodel.NormalizeRecord)
// before returning: since I18N-02 (issue #485) the write boundary normalizes
// every stored contact to NFC, so a record the property suites then persist
// must already be in storage form, or the populate round-trip comparisons
// fail on the (deliberately NFD) unicodeText samples. The NFD->NFC cross-
// encoding behavior itself is exercised by the dedicated Unicode tests, which
// write NFD bytes straight to the columns; the property corpus mirrors what a
// real ingress produces.
func Record(t *rapid.T) *contactmodel.Record {
	return contactmodel.NormalizeRecord(&contactmodel.Record{
		Card:        Card(t),
		Envelope:    Envelope(t),
		Passthrough: Passthrough(t),
	})
}

// Records draws n generated records with distinct card UIDs, suitable for
// populating a database without tripping the partial unique VCardUID index.
func Records(t *rapid.T, n int) []*contactmodel.Record {
	out := make([]*contactmodel.Record, 0, n)
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		rec := Record(t)
		for seen[rec.Card.UID] || rec.Card.UID == "" {
			rec.Card.UID = uid(t)
		}
		seen[rec.Card.UID] = true
		out = append(out, rec)
	}
	return out
}
