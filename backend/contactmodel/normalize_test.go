package contactmodel

import (
	"encoding/json"
	"reflect"
	"testing"
)

const (
	nfdJose   = "Jos\u0065\u0301" // "Jos" + e + U+0301 (combining acute)
	nfdCafe   = "Caf\u0065\u0301" // "Caf" + e + U+0301
	nfdGarcia = "Garci\u0301a"    // "Garci" + U+0301 + "a" -> NFC "García"
	priya     = "\u092a\u094d\u0930\u093f\u092f\u093e \u0936\u0930\u094d\u092e\u093e"
)

// richRecord returns a Card-heavy Record touching every normalized field, in
// NFC form, so the identity test doubles as a "must not over-normalize" guard
// over the same shapes the canonical TEST-02 fixture exercises (accented
// Latin, non-Latin scripts, Devanagari with intrinsic combining marks, and a
// ZWJ emoji sequence).
func richRecord() *Record {
	pref := 1
	return &Record{
		Card: Card{
			UID:  "urn:uuid:nfc-or-not",
			Kind: "individual",
			Name: &Name{
				Components: []NameComponent{
					{Kind: "given", Value: "Björk"},
					{Kind: "surname", Value: "Guðmundsdóttir", Phonetic: "gvʏð"},
				},
				Full:   "Björk Guðmundsdóttir",
				SortAs: map[string]string{"given": "björk", "surname": "guðmundsdóttir"},
			},
			Nicknames: []Nickname{{ID: "n1", Name: "joão", Contexts: []string{"private"}, Pref: &pref}},
			Organizations: []Organization{{
				ID: "o1", Name: "Östgöta Nation", SortAs: "ostgota",
				Units: []OrgUnit{{Name: "Förvaltningen", SortAs: "forvaltningen"}},
			}},
			Titles: []Title{{ID: "t1", Name: "Direktør", Kind: "title"}},
			Emails: []Email{{ID: "e1", Address: "josé@example.com", Label: "Prié"}},
			Phones: []Phone{{ID: "p1", Number: "+1 555 0100", Features: []string{"voice"}, Label: "Føste"}},
			ImppAddresses: []OnlineService{{
				ID: "i1", Service: "Mastodon", URI: "https://café.example/@bjork", User: "Björk",
			}},
			SocialProfiles: []OnlineService{{ID: "s1", Service: "X", URI: "urn:x:a", User: "guðrún"}},
			Addresses: []Address{{
				ID:   "a1",
				Full: "Sankt Jacobs Kirkeplads 1, 2100 København Ø",
				Components: []AddressComponent{
					{Kind: "number", Value: "1"},
					{Kind: "street", Value: "Sankt Jacobs Kirkeplads"},
					{Kind: "postcode", Value: "2100"},
					{Kind: "locality", Value: "København Ø"},
				},
				CountryCode: "DK",
			}},
			Anniversaries: []Anniversary{{
				ID:   "an1",
				Kind: "birth",
				Place: &Address{
					Full:       "Reykjavík",
					Components: []AddressComponent{{Kind: "locality", Value: "Reykjavík"}},
				},
			}},
			PersonalInfo: []PersonalInfo{{ID: "pi1", Kind: "hobby", Value: "Skið", Level: "high"}},
			SpeakToAs: &SpeakToAs{
				GrammaticalGenders: []GrammaticalGender{
					{ID: "gg1", Value: "feminine", Language: "is"},
				},
				Pronouns: []Pronouns{{ID: "pr1", Pronouns: "hún/hana", Contexts: []string{"private"}}},
			},
			Notes: []Note{{
				ID: "no1", Note: "Café " + zwjFamily,
				Author: &Author{Name: "Guðrún", URI: "urn:x:author"},
			}},
			Keywords: []string{"köp", "sälj"},
			Media: []Resource{{
				ID: "m1", Kind: "photo", URI: "data:image/png;base64,QUJD", Label: "Bíld",
			}},
			Links:   []Resource{{ID: "l1", URI: "https://café.example/", Label: "Café"}},
			Members: []string{"urn:uuid:member"},
		},
		Envelope: CRMEnvelope{
			Kind:               "human",
			HowWeMet:           "M\u00e9tumst vi\u00f0 kaffi",
			WorkInformation:    "Akureyrarhöfn",
			ContactInformation: "Sævarhöfði 14",
			Gender:             "hún",
			Circles:            []string{"fjölskylda"},
		},
		Passthrough: Passthrough{
			VCard: []JCardProp{{
				Name: "X-UNKNOWN", Type: "text",
				Value: json.RawMessage(`"Caf\u00e9"`),
			}},
		},
		UID: "db-uid",
	}
}

const zwjFamily = "\U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466" // 👨👩👧👦

func TestNormalizeRecord_NFDBecomesNFC(t *testing.T) {
	in := &Record{Card: Card{
		Name: &Name{
			Full: nfdJose,
			Components: []NameComponent{
				{Kind: "given", Value: nfdJose},
				{Kind: "surname", Value: nfdGarcia},
			},
		},
		Nicknames: []Nickname{{Name: nfdCafe}},
		Emails:    []Email{{Address: "jos\u00e9@example.com"}},
		Notes:     []Note{{Note: nfdCafe}},
		Keywords:  []string{nfdJose},
	}}
	out := NormalizeRecord(in)

	if got := out.Card.Name.Full; got != "José" {
		t.Errorf("Full = %q, want NFC Jos\\u00e9", got)
	}
	if got := out.Card.Name.Components[0].Value; got != "José" {
		t.Errorf("given = %q, want Jos\\u00e9", got)
	}
	if got := out.Card.Name.Components[1].Value; got != "García" {
		t.Errorf("surname = %q, want Garc\\u00eda", got)
	}
	if got := out.Card.Nicknames[0].Name; got != "Café" {
		t.Errorf("nickname = %q, want Caf\\u00e9", got)
	}
	if got := out.Card.Notes[0].Note; got != "Café" {
		t.Errorf("note = %q, want Caf\\u00e9", got)
	}
	if got := out.Card.Keywords[0]; got != "José" {
		t.Errorf("keyword = %q, want Jos\\u00e9", got)
	}
	if n := len([]byte(out.Card.Name.Full)); n != len("José") {
		t.Errorf("expected 5 bytes for NFC José, got %d (%q)", n, out.Card.Name.Full)
	}
}

func TestNormalizeRecord_DoesNotMutateInput(t *testing.T) {
	in := &Record{Card: Card{Name: &Name{Full: nfdJose}}}
	NormalizeRecord(in)
	if in.Card.Name.Full != nfdJose {
		t.Errorf("input mutated: got %q, want original NFD bytes", in.Card.Name.Full)
	}
}

func TestNormalizeRecord_IdentityOnNFC(t *testing.T) {
	r := richRecord()
	before := *r
	out := NormalizeRecord(r)
	if !reflect.DeepEqual(&before, out) {
		t.Errorf("NFC record changed under NFC normalization:\nbefore=%+v\nafter =%+v", &before, out)
	}
}

func TestNormalizeRecord_Idempotent(t *testing.T) {
	r := &Record{Card: Card{
		Name: &Name{Full: nfdJose, Components: []NameComponent{{Kind: "given", Value: nfdJose}}},
	}}
	once := NormalizeRecord(r)
	twice := NormalizeRecord(once)
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("second normalization changed bytes:\nonce =%+v\ntwice=%+v", once, twice)
	}
}

func TestNormalizeRecord_PreservesIdentityURIsAndOpaqueData(t *testing.T) {
	in := richRecord()
	uid, linkURI, mediaURI, member := in.Card.UID, in.Card.Links[0].URI, in.Card.Media[0].URI, in.Card.Members[0]
	passRaw := string(in.Passthrough.VCard[0].Value)
	// A decomposed IRI (é as e + U+0301) is address data, not display text —
	// normalization must leave it alone.
	decomposedIRI := "https://\u0065\u0301.example/x"
	in.Card.ImppAddresses[0].URI = decomposedIRI
	iribefore := in.Card.ImppAddresses[0].URI

	out := NormalizeRecord(in)

	if out.Card.UID != uid {
		t.Errorf("UID changed: %q -> %q", uid, out.Card.UID)
	}
	if out.Card.Links[0].URI != linkURI {
		t.Errorf("link URI changed: %q -> %q", linkURI, out.Card.Links[0].URI)
	}
	if out.Card.Media[0].URI != mediaURI {
		t.Errorf("media URI changed: %q -> %q", mediaURI, out.Card.Media[0].URI)
	}
	if out.Card.Members[0] != member {
		t.Errorf("member URI changed: %q -> %q", member, out.Card.Members[0])
	}
	if out.Card.ImppAddresses[0].URI != iribefore {
		t.Errorf("decomposed IRI changed: %q -> %q", iribefore, out.Card.ImppAddresses[0].URI)
	}
	if string(out.Passthrough.VCard[0].Value) != passRaw {
		t.Errorf("passthrough bytes changed")
	}
}

func TestNormalizeRecord_NilSafe(t *testing.T) {
	if NormalizeRecord(nil) != nil {
		t.Fatal("NormalizeRecord(nil) must return nil")
	}
}

// TestNormalizeRecord_NilNestedPointersIsASafeNoOp covers the nil-Name and
// nil-SpeakToAs branches (a bare Record{Envelope: ...} with no Card sections).
func TestNormalizeRecord_NilNestedPointersIsASafeNoOp(t *testing.T) {
	out := NormalizeRecord(&Record{})
	if out.Card.Name != nil || out.Card.SpeakToAs != nil {
		t.Fatalf("nil nested pointers must stay nil: %+v", out)
	}
	withGender := &Record{Card: Card{
		SpeakToAs: &SpeakToAs{
			GrammaticalGenders: []GrammaticalGender{{ID: "g", Value: "feminine", Language: "de"}},
		},
	}}
	if got := NormalizeRecord(withGender).Card.SpeakToAs.GrammaticalGenders[0].Value; got != "feminine" {
		t.Fatalf("grammatical-gender token changed: %q", got)
	}
}

func TestNormalizeRecord_ZwjAndIntrinsicCombiningMarksSurvive(t *testing.T) {
	r := &Record{Card: Card{
		Name:  &Name{Components: []NameComponent{{Kind: "given", Value: priya}}},
		Notes: []Note{{Note: "family " + zwjFamily}},
	}}
	out := NormalizeRecord(r)
	if got := out.Card.Name.Components[0].Value; got != priya {
		t.Errorf("Devanagari (intrinsically-combining, already NFC) changed: %q", got)
	}
	if got := out.Card.Notes[0].Note; got != "family "+zwjFamily {
		t.Errorf("ZWJ family emoji sequence changed: %q", got)
	}
}
