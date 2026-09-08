package contactmodel

import "golang.org/x/text/unicode/norm"

// NormalizeRecord returns a copy of r with every free-text and name string in
// the standardized Card and CRM envelope normalized to Unicode NFC (issue
// #485, I18N-02). It never mutates r itself: callers that keep the Record
// around after applying it (import preview rows, CardDAV comparison state)
// keep seeing the exact bytes they supplied.
//
// Why NFC, at this boundary, on write:
//
//   - A grapheme like "é" has two byte spellings — the precomposed U+00E9
//     (NFC) and "e" + U+0301 (NFD). macOS/iOS produce NFD in several paths;
//     nearly everything else produces NFC. They render identically, so a
//     contact imported from one device and searched from another must compare
//     as equal, and two spellings of one person must not survive as separate
//     rows that duplicate detection never pairs (the name keys were
//     byte-distinct).
//   - The FTS5 tokenizer already folds NFC/NFD for search (combining marks
//     tokenize as separators), but the LIKE-only search arms, duplicate
//     detection, sort keys, CardDAV byte comparison, and the Android offline
//     mirror are all byte comparisons. Normalizing the stored text is what
//     makes every one of those consumers see one canonical form; normalizing
//     only on read would leave the stored data inconsistent.
//   - NFC is the conventional storage form (the web, HTML5, XML, most
//     operating systems) and the whole canonical TEST-02 fixture corpus is
//     already NFC, so this function is an identity on it — a byte-exact
//     round-trip guard against over-normalizing.
//
// Normalization is deliberately scoped to free-text and name fields the
// project owns and understands: name components, full names, nicknames,
// organizations, titles, labels, addresses, notes, keywords, pronouns, and
// the CRM free-text envelope fields. It does NOT touch:
//
//   - card.uid — sync identity; changing bytes here would orphan the contact's
//     vCard_uid and break CardDAV/household/relationship linkage.
//   - URIs (links, calendars, IMPP, media, members, relatedTo targets,
//     coordinates, contactUris) — an IRI is address data, not display text.
//   - opaque Passthrough / Localizations content — unknown properties are
//     preserved verbatim by design (lossless pass-through), and rewriting
//     their bytes would be guessing.
//   - timestamps, language tags, and closed-vocabulary tokens (kinds,
//     contexts, media types, phonetic-system codes) — ASCII-identical under
//     NFC, so normalizing them is meaningless.
//
// It is idempotent: NFC(NFC(s)) == NFC(s). Rerunning it over already-clean
// rows is a no-op, which is what lets the startup backfill (see
// backend/models/normalize_backfill.go) be interruption-safe.
func NormalizeRecord(r *Record) *Record {
	if r == nil {
		return nil
	}
	out := *r
	out.Card = normalizeCard(r.Card)
	out.Envelope = normalizeEnvelope(r.Envelope)
	// Passthrough and Record.UID/ETag are left byte-identical by design.
	return &out
}

func normalizeCard(c Card) Card {
	c.Name = normalizeName(c.Name)
	c.Nicknames = mapSlice(c.Nicknames, func(n Nickname) Nickname {
		n.Name = norm.NFC.String(n.Name)
		return n
	})
	c.Organizations = mapSlice(c.Organizations, func(o Organization) Organization {
		o.Name = norm.NFC.String(o.Name)
		o.SortAs = norm.NFC.String(o.SortAs)
		o.Units = mapSlice(o.Units, func(u OrgUnit) OrgUnit {
			u.Name = norm.NFC.String(u.Name)
			u.SortAs = norm.NFC.String(u.SortAs)
			return u
		})
		return o
	})
	c.Titles = mapSlice(c.Titles, func(t Title) Title {
		t.Name = norm.NFC.String(t.Name)
		return t
	})
	c.Emails = mapSlice(c.Emails, func(e Email) Email {
		e.Address = norm.NFC.String(e.Address)
		e.Label = norm.NFC.String(e.Label)
		return e
	})
	c.Phones = mapSlice(c.Phones, func(p Phone) Phone {
		p.Number = norm.NFC.String(p.Number)
		p.Label = norm.NFC.String(p.Label)
		return p
	})
	c.ImppAddresses = mapSlice(c.ImppAddresses, normalizeOnlineService)
	c.SocialProfiles = mapSlice(c.SocialProfiles, normalizeOnlineService)
	c.OtherOnlineServices = mapSlice(c.OtherOnlineServices, normalizeOnlineService)
	c.Addresses = mapSlice(c.Addresses, func(a Address) Address {
		a.Components = mapSlice(a.Components, func(ac AddressComponent) AddressComponent {
			ac.Value = norm.NFC.String(ac.Value)
			ac.Phonetic = norm.NFC.String(ac.Phonetic)
			return ac
		})
		a.Full = norm.NFC.String(a.Full)
		a.TimeZone = norm.NFC.String(a.TimeZone)
		// CountryCode (vCard CC), Coordinates (geo: URI) and the phonetic
		// system/script tags are codes, not display text — left as-is.
		return a
	})
	c.Anniversaries = mapSlice(c.Anniversaries, func(a Anniversary) Anniversary {
		if a.Place != nil {
			place := normalizeAddress(*a.Place)
			a.Place = &place
		}
		return a
	})
	c.SpeakToAs = normalizeSpeakToAs(c.SpeakToAs)
	c.PersonalInfo = mapSlice(c.PersonalInfo, func(p PersonalInfo) PersonalInfo {
		p.Value = norm.NFC.String(p.Value)
		p.Label = norm.NFC.String(p.Label)
		return p
	})
	c.Notes = mapSlice(c.Notes, func(n Note) Note {
		n.Note = norm.NFC.String(n.Note)
		if n.Author != nil {
			author := *n.Author
			author.Name = norm.NFC.String(author.Name)
			n.Author = &author
		}
		return n
	})
	c.Keywords = mapSlice(c.Keywords, norm.NFC.String)
	// Media/Calendars/FreeBusyURLs/SchedulingAddresses/CryptoKeys/Directories/
	// Links/ContactURIs carry URIs in Resource.URI — preserved verbatim; their
	// Label field is display text, so it is normalized. RelatedTo targets and
	// Members are URIs/uids — preserved. PreferredLanguages carry language
	// tags — preserved.
	c.Media = mapSlice(c.Media, normalizeResource)
	c.Calendars = mapSlice(c.Calendars, normalizeResource)
	c.FreeBusyURLs = mapSlice(c.FreeBusyURLs, normalizeResource)
	c.SchedulingAddresses = mapSlice(c.SchedulingAddresses, normalizeResource)
	c.CryptoKeys = mapSlice(c.CryptoKeys, normalizeResource)
	c.Directories = mapSlice(c.Directories, normalizeResource)
	c.Links = mapSlice(c.Links, normalizeResource)
	c.ContactURIs = mapSlice(c.ContactURIs, normalizeResource)
	return c
}

func normalizeName(n *Name) *Name {
	if n == nil {
		return nil
	}
	nn := *n
	nn.Components = mapSlice(nn.Components, func(nc NameComponent) NameComponent {
		nc.Value = norm.NFC.String(nc.Value)
		nc.Phonetic = norm.NFC.String(nc.Phonetic)
		return nc
	})
	nn.Full = norm.NFC.String(nn.Full)
	if nn.SortAs != nil {
		m := make(map[string]string, len(nn.SortAs))
		for k, v := range nn.SortAs {
			m[k] = norm.NFC.String(v)
		}
		nn.SortAs = m
	}
	return &nn
}

func normalizeOnlineService(s OnlineService) OnlineService {
	// s.URI is address data, preserved verbatim; service/user/label are
	// display text.
	s.Service = norm.NFC.String(s.Service)
	s.User = norm.NFC.String(s.User)
	s.Label = norm.NFC.String(s.Label)
	return s
}

func normalizeResource(res Resource) Resource {
	res.Label = norm.NFC.String(res.Label)
	return res
}

func normalizeSpeakToAs(s *SpeakToAs) *SpeakToAs {
	if s == nil {
		return nil
	}
	ss := *s
	ss.GrammaticalGenders = mapSlice(ss.GrammaticalGenders, func(g GrammaticalGender) GrammaticalGender {
		return g // value is a closed vocabulary token; language is a tag
	})
	ss.Pronouns = mapSlice(ss.Pronouns, func(p Pronouns) Pronouns {
		p.Pronouns = norm.NFC.String(p.Pronouns)
		return p
	})
	return &ss
}

func normalizeEnvelope(e CRMEnvelope) CRMEnvelope {
	e.HowWeMet = norm.NFC.String(e.HowWeMet)
	e.WorkInformation = norm.NFC.String(e.WorkInformation)
	e.ContactInformation = norm.NFC.String(e.ContactInformation)
	e.Gender = norm.NFC.String(e.Gender)
	return e
}

// normalizeAddress is the address half of normalizeCard, reused for
// Anniversary.Place.
func normalizeAddress(a Address) Address {
	a.Components = mapSlice(a.Components, func(ac AddressComponent) AddressComponent {
		ac.Value = norm.NFC.String(ac.Value)
		ac.Phonetic = norm.NFC.String(ac.Phonetic)
		return ac
	})
	a.Full = norm.NFC.String(a.Full)
	a.TimeZone = norm.NFC.String(a.TimeZone)
	return a
}

// mapSlice maps each element of a slice to a new slice. It is the tiny
// functional helper the walker above composes out of; keeping the per-type
// closures local makes each field's normalization decision visible at its
// declaration.
func mapSlice[S ~[]E, E any](s S, f func(E) E) S {
	if len(s) == 0 {
		return s
	}
	out := make(S, len(s))
	for i := range s {
		out[i] = f(s[i])
	}
	return out
}
