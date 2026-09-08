package models

import (
	"testing"

	"mycorrhizal/contactmodel"

	"golang.org/x/text/unicode/norm"
)

// I18N-02 (issue #485): ApplyRecordToContact is the single Record->Contact
// write boundary for REST create/update, VCF/JSContact import, and CardDAV
// put/reconcile. NFC-normalizing there means NFD (decomposed) text can no
// longer enter stored contact bytes through any ingress path. These tests pin
// the boundary: an NFD name in lands as NFC out, in both the neutral Card and
// the flat projections that feed the search/duplicate/sort columns.
func TestApplyRecordToContact_NormalizesNFDRecordToNFC(t *testing.T) {
	rec := &contactmodel.Record{
		Card: contactmodel.Card{
			Name: &contactmodel.Name{
				Full: "Jos\u0065\u0301 Garci\u0301a",
				Components: []contactmodel.NameComponent{
					{Kind: "given", Value: "Jos\u0065\u0301"},
					{Kind: "surname", Value: "Garci\u0301a"},
				},
			},
			Addresses: []contactmodel.Address{{
				Components: []contactmodel.AddressComponent{
					{Kind: "name", Value: "Cafe\u0301 Street"}, // JSContact street = kind "name"
					{Kind: "locality", Value: "San Jos\u0065\u0301"},
				},
			}},
		},
	}
	var c Contact
	ApplyRecordToContact(&c, rec, "")
	c.deriveDenormalized() // what the save immediately after ApplyRecordToContact does

	if c.Card.Name.Full != "José García" {
		t.Errorf("Card.Name.Full = %q, want NFC 'José García'", c.Card.Name.Full)
	}
	if c.Firstname != "José" {
		t.Errorf("Firstname = %q, want NFC 'José'", c.Firstname)
	}
	if c.Lastname != "García" {
		t.Errorf("Lastname = %q, want NFC 'García'", c.Lastname)
	}
	if c.FN != "José García" {
		t.Errorf("FN = %q, want NFC 'José García'", c.FN)
	}
	wantFlat := "Café Street, San José" // street/locality are flattened onto addresses_flat
	if !norm.NFC.IsNormalString(c.AddressesFlat) {
		t.Errorf("AddressesFlat is not NFC: %q", c.AddressesFlat)
	}
	if c.AddressesFlat != wantFlat {
		t.Errorf("AddressesFlat = %q, want %q", c.AddressesFlat, wantFlat)
	}
	// DeriveSortName runs over the NFC flat values, so the sort key cannot be
	// encoding-dependent.
	if want := "garcía"; c.SortName != want {
		t.Errorf("SortName = %q, want %q", c.SortName, want)
	}
}

func TestApplyRecordToContact_InputRecordNotMutated(t *testing.T) {
	rec := &contactmodel.Record{
		Card: contactmodel.Card{Name: &contactmodel.Name{Full: "Jos\u0065\u0301"}},
	}
	var c Contact
	ApplyRecordToContact(&c, rec, "")
	c.deriveDenormalized()
	if rec.Card.Name.Full != "Jos\u0065\u0301" {
		t.Errorf("input Record mutated: %q", rec.Card.Name.Full)
	}
}

// TestApplyRecordToContact_DoesNotTouchNFCRoundTrip guards the other
// direction: a canonical (already-NFC) record must pass through byte-exact —
// the canonical TEST-02 fixture corpus is NFC, so normalizing must never
// rewrite it. Devanagari (intrinsically-combining) and a ZWJ emoji sequence
// are included because they carry combining characters that must not be
// reordered or dropped.
func TestApplyRecordToContact_DoesNotTouchNFCRoundTrip(t *testing.T) {
	rec := &contactmodel.Record{
		Card: contactmodel.Card{
			Name: &contactmodel.Name{
				Full: "प्रिया शर्मा 👨\u200d👩\u200d👧\u200d👦",
				Components: []contactmodel.NameComponent{
					{Kind: "given", Value: "प्रिया"},
					{Kind: "surname", Value: "शर्मा"},
				},
			},
		},
	}
	var c Contact
	ApplyRecordToContact(&c, rec, "")

	if c.Card.Name.Full != "प्रिया शर्मा 👨\u200d👩\u200d👧\u200d👦" {
		t.Errorf("NFC Card mutated on apply: %q", c.Card.Name.Full)
	}
	if c.Firstname != "प्रिया" || c.Lastname != "शर्मा" {
		t.Errorf("flat projections changed: %q / %q", c.Firstname, c.Lastname)
	}
}
