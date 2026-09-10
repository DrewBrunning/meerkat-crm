---
title: Field Compatibility Matrix
nav_order: 15
---

# DATA-01 — Field compatibility matrix

> **Generated artifact — do not hand-edit.** The source of truth is the locked
> correspondence oracle (`backend/correspondence/testdata/correspondence.tsv`, ADR-0002)
> plus the issue #515 canonical-field audit for fields with no correspondence row.
> Regenerate with `cd backend && go run ./cmd/gencompatmatrix` (or `make gen-compat-matrix`);
> the drift test `backend/correspondence/matrix_test.go` fails if this file and the
> generator disagree.

Every canonical field, classified per format into the v0.6.5 milestone's five buckets.
No classification here encodes a mapping absent from the correspondence table
(ADR-0002): buckets derive from the table's own columns and notes.

## Bucket legend

| Bucket | Meaning |
|---|---|
| **exact** | Round-trips with no transform (identity). |
| **transformed** | Round-trips through the table's declared value transform. |
| **extended** | Carried via an `X-` property or a JSContact extension / passthrough escape hatch (issue #514). |
| **unsupported** | No home in that format; dropped with a warn diagnostic per ADR-0002. |
| **lossy** | Survives but with reduced fidelity (precision, structure, or cardinality). |

## CardDAV-on-the-wire

The CardDAV column is not a fourth format: the server advertises `text/vcard` 3.0 and
4.0 (`backend/carddav/backend.go`) and negotiates per request via the HTTP `Accept`
header (RFC 6352 §10.4.1), **defaulting to vCard 4.0** when the client sends no
`version=`. Each CardDAV cell therefore repeats the vCard 4.0 classification and notes
the vCard 3.0 classification where it differs. Unsupported/lossy *loss reports*
(DATA-02, issue #442) exist per serialized format — vCard 4.0, vCard 3.0, JSContact —
never for the CardDAV carrier itself.

## Matrix — correspondence concepts

| Concept | Neutral path | Transform | vCard 4.0 | vCard 3.0 | JSContact | CardDAV (default v4) |
|---|---|---|---|---|---|---|
| `uid` | `Card.UID` | `identity` | **exact** · `UID` | **exact** · `UID` | **exact** · `/uid` | **exact** · `UID` |
| `kind` | `Card.Kind` | `identity` | **exact** · `KIND` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/kind` | **exact** · `KIND` — v3: unsupported |
| `prodid` | `Card.ProdID` | `identity` | **exact** · `PRODID` | **exact** · `PRODID` | **exact** · `/prodId` | **exact** · `PRODID` |
| `updated` | `Card.Updated` | `ts_rfc3339` | **transformed** · `REV` | **transformed** · `REV` | **transformed** · `/updated` | **transformed** · `REV` |
| `created` | `Card.Created` | `ts_rfc3339` | **transformed** · `CREATED` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/created` | **transformed** · `CREATED` — v3: unsupported |
| `language` | `Card.Language` | `identity` | **exact** · `LANGUAGE` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/language` | **exact** · `LANGUAGE` — v3: unsupported |
| `name.full` | `Card.Name.Full` | `identity` | **exact** · `FN` | **exact** · `FN` | **exact** · `/name/full` | **exact** · `FN` |
| `name.surname` | `Card.Name.Components[kind=surname].Value` | `n_component` | **transformed** · `N` | **transformed** · `N` | **transformed** · `/name/components` | **transformed** · `N` |
| `name.given` | `Card.Name.Components[kind=given].Value` | `n_component` | **transformed** · `N` | **transformed** · `N` | **transformed** · `/name/components` | **transformed** · `N` |
| `name.given2` | `Card.Name.Components[kind=given2].Value` | `n_component` | **transformed** · `N` | **transformed** · `N` | **transformed** · `/name/components` | **transformed** · `N` |
| `name.title` | `Card.Name.Components[kind=title].Value` | `n_component` | **transformed** · `N` | **transformed** · `N` | **transformed** · `/name/components` | **transformed** · `N` |
| `name.credential` | `Card.Name.Components[kind=credential].Value` | `n_component` | **transformed** · `N` | **transformed** · `N` | **transformed** · `/name/components` | **transformed** · `N` |
| `name.surname2` | `Card.Name.Components[kind=surname2].Value` | `n_component` | **transformed** · `N` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/name/components` | **transformed** · `N` — v3: unsupported |
| `name.generation` | `Card.Name.Components[kind=generation].Value` | `n_component` | **transformed** · `N` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/name/components` | **transformed** · `N` — v3: unsupported |
| `name.phonetic` | `Card.Name.PhoneticScript` | `identity` | **exact** · `N` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/name/phoneticScript` | **exact** · `N` — v3: unsupported |
| `nickname` | `Card.Nicknames[].Name` | `identity` | **exact** · `NICKNAME` | **exact** · `NICKNAME` | **exact** · `/nicknames/{id}/name` | **exact** · `NICKNAME` |
| `org` | `Card.Organizations[].Name` | `org_units` | **transformed** · `ORG` | **transformed** · `ORG` | **transformed** · `/organizations/{id}/name` | **transformed** · `ORG` |
| `org.unit` | `Card.Organizations[].Units[].Name` | `org_units` | **transformed** · `ORG` | **transformed** · `ORG` | **transformed** · `/organizations/{id}/units` | **transformed** · `ORG` |
| `title` | `Card.Titles[kind=title].Name` | `identity` | **exact** · `TITLE` | **exact** · `TITLE` | **exact** · `/titles/{id}/name` | **exact** · `TITLE` |
| `role` | `Card.Titles[kind=role].Name` | `identity` | **exact** · `ROLE` | **exact** · `ROLE` | **exact** · `/titles/{id}/name` | **exact** · `ROLE` |
| `email` | `Card.Emails[].Address` | `identity` | **exact** · `EMAIL` | **exact** · `EMAIL` | **exact** · `/emails/{id}/address` | **exact** · `EMAIL` |
| `phone` | `Card.Phones[].Number` | `identity` | **exact** · `TEL` | **exact** · `TEL` | **exact** · `/phones/{id}/number` | **exact** · `TEL` |
| `impp` | `Card.ImppAddresses[].URI` | `identity` | **exact** · `IMPP` | **exact** · `IMPP` | **exact** · `/onlineServices/{id}/uri` | **exact** · `IMPP` |
| `social` | `Card.SocialProfiles[].Service` | `onlineservice` | **transformed** · `SOCIALPROFILE` | **extended** · `X-SOCIALPROFILE` — X- property | **transformed** · `/onlineServices/{id}` | **transformed** · `SOCIALPROFILE` — v3: extended |
| `adr` | `Card.Addresses[]` | `adr_components` | **transformed** · `ADR` | **lossy** · `ADR` — v3 ADR has only the 7 legacy fields; RFC 9553/9554 component kinds beyond those and the CC parameter are dropped with a warn | **transformed** · `/addresses/{id}` | **transformed** · `ADR` — v3: lossy |
| `adr.geo` | `Card.Addresses[].Coordinates` | `geo_uri` | **transformed** · `ADR` | **transformed** · `GEO` — no ADR GEO param in v3; emitted as a separate GEO property (lat;lon) | **transformed** · `/addresses/{id}/coordinates` | **transformed** · `ADR` |
| `adr.tz` | `Card.Addresses[].TimeZone` | `identity` | **exact** · `ADR` | **transformed** · `TZ` — no ADR TZ param in v3; emitted as a separate TZ property | **exact** · `/addresses/{id}/timeZone` | **exact** · `ADR` — v3: transformed |
| `anniversary.birth` | `Card.Anniversaries[kind=birth].Date` | `date_partial` | **transformed** · `BDAY` | **lossy** · `BDAY` — v3 BDAY is date-only; time-of-day dropped (warns) | **transformed** · `/anniversaries/{id}/date` | **transformed** · `BDAY` — v3: lossy |
| `anniversary.wedding` | `Card.Anniversaries[kind=wedding].Date` | `date_partial` | **transformed** · `ANNIVERSARY` | **extended** · `X-ANNIVERSARY` — v3 has no ANNIVERSARY property; adapter emits X-ANNIVERSARY with a warn | **transformed** · `/anniversaries/{id}/date` | **transformed** · `ANNIVERSARY` — v3: extended |
| `anniversary.death` | `Card.Anniversaries[kind=death].Date` | `date_partial` | **transformed** · `DEATHDATE` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/anniversaries/{id}/date` | **transformed** · `DEATHDATE` — v3: unsupported |
| `anniversary.place.birth` | `Card.Anniversaries[kind=birth].Place` | `place_text` | **lossy** · `BIRTHPLACE` — Address structure flattened to BIRTHPLACE TEXT/URI (RFC 6474 §2.1); structure lossy, warns | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/anniversaries/{id}/place` | **lossy** · `BIRTHPLACE` — v3: unsupported |
| `anniversary.place.death` | `Card.Anniversaries[kind=death].Place` | `place_text` | **lossy** · `DEATHPLACE` — Address structure flattened to DEATHPLACE TEXT/URI (RFC 6474 §2.2); structure lossy, warns | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/anniversaries/{id}/place` | **lossy** · `DEATHPLACE` — v3: unsupported |
| `gramgender` | `Card.SpeakToAs.GrammaticalGenders[].Value` | `enum_lower` | **transformed** · `GRAMGENDER` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **lossy** · `/speakToAs/grammaticalGender` — JSContact speakToAs.grammaticalGender is scalar (RFC 9553 §2.2.4); multiple neutral entries collapse to the language-selected/first entry (warns) | **transformed** · `GRAMGENDER` — v3: unsupported |
| `pronouns` | `Card.SpeakToAs.Pronouns[].Pronouns` | `identity` | **exact** · `PRONOUNS` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/speakToAs/pronouns/{id}/pronouns` | **exact** · `PRONOUNS` — v3: unsupported |
| `expertise` | `Card.PersonalInfo[kind=expertise]` | `personalinfo` | **transformed** · `EXPERTISE` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/personalInfo/{id}` | **transformed** · `EXPERTISE` — v3: unsupported |
| `hobby` | `Card.PersonalInfo[kind=hobby]` | `personalinfo` | **transformed** · `HOBBY` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/personalInfo/{id}` | **transformed** · `HOBBY` — v3: unsupported |
| `interest` | `Card.PersonalInfo[kind=interest]` | `personalinfo` | **transformed** · `INTEREST` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **transformed** · `/personalInfo/{id}` | **transformed** · `INTEREST` — v3: unsupported |
| `note` | `Card.Notes[].Note` | `identity` | **exact** · `NOTE` | **lossy** · `NOTE` — v3 NOTE drops the AUTHOR/AUTHOR-NAME/CREATED params (RFC 9554-only; warns) | **exact** · `/notes/{id}/note` | **exact** · `NOTE` — v3: lossy |
| `keywords` | `Card.Keywords` | `csv_join` | **transformed** · `CATEGORIES` | **transformed** · `CATEGORIES` | **lossy** · `/keywords` — JSContact keywords is a boolean-set; duplicate keywords collapse (warns) | **transformed** · `CATEGORIES` |
| `photo` | `Card.Media[kind=photo].URI` | `media_uri` | **transformed** · `PHOTO` | **transformed** · `PHOTO` | **transformed** · `/media/{id}/uri` | **transformed** · `PHOTO` |
| `logo` | `Card.Media[kind=logo].URI` | `media_uri` | **transformed** · `LOGO` | **transformed** · `LOGO` | **transformed** · `/media/{id}/uri` | **transformed** · `LOGO` |
| `sound` | `Card.Media[kind=sound].URI` | `media_uri` | **transformed** · `SOUND` | **transformed** · `SOUND` | **transformed** · `/media/{id}/uri` | **transformed** · `SOUND` |
| `calendar` | `Card.Calendars[].URI` | `identity` | **exact** · `CALURI` | **exact** · `CALURI` | **exact** · `/calendars/{id}/uri` | **exact** · `CALURI` |
| `freebusy` | `Card.FreeBusyURLs[].URI` | `identity` | **exact** · `FBURL` | **exact** · `FBURL` | **exact** · `/calendars/{id}/uri` | **exact** · `FBURL` |
| `caladruri` | `Card.SchedulingAddresses[].URI` | `identity` | **exact** · `CALADRURI` | **exact** · `CALADRURI` | **exact** · `/schedulingAddresses/{id}/uri` | **exact** · `CALADRURI` |
| `key` | `Card.CryptoKeys[].URI` | `identity` | **exact** · `KEY` | **exact** · `KEY` | **exact** · `/cryptoKeys/{id}/uri` | **exact** · `KEY` |
| `directory` | `Card.Directories[kind=directory].URI` | `identity` | **exact** · `ORG-DIRECTORY` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/directories/{id}/uri` | **exact** · `ORG-DIRECTORY` — v3: unsupported |
| `source` | `Card.Directories[kind=entry].URI` | `identity` | **exact** · `SOURCE` | **exact** · `SOURCE` | **exact** · `/directories/{id}/uri` | **exact** · `SOURCE` |
| `link` | `Card.Links[].URI` | `identity` | **exact** · `URL` | **exact** · `URL` | **exact** · `/links/{id}/uri` | **exact** · `URL` |
| `contacturi` | `Card.ContactURIs[].URI` | `identity` | **exact** · `CONTACT-URI` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/links/{id}/uri` | **exact** · `CONTACT-URI` — v3: unsupported |
| `lang` | `Card.PreferredLanguages[].Language` | `identity` | **exact** · `LANG` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/preferredLanguages/{id}/language` | **exact** · `LANG` — v3: unsupported |
| `related` | `Card.RelatedTo[]` | `related` | **transformed** · `RELATED` | **lossy** · `AGENT` — v3 has no RELATED property; target redirected to AGENT with a warn, relation TYPE tokens lost | **transformed** · `/relatedTo/{target}` | **transformed** · `RELATED` — v3: lossy |
| `member` | `Card.Members` | `identity` | **exact** · `MEMBER` | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `/members` | **exact** · `MEMBER` — v3: unsupported |
| `pt.vcard` | `Passthrough.VCard` | `passthrough_vcard` | **extended** · `*verbatim*` — passthrough escape hatch: stored jCard props re-emitted verbatim | **extended** · `*verbatim*` — passthrough escape hatch: stored jCard props re-emitted verbatim | **extended** · `/vCardProps` — carried via the JSContact vCardProps extension member (RFC 9555) | **extended** · `*verbatim*` — passthrough escape hatch: stored jCard props re-emitted verbatim |
| `pt.jscontact` | `Passthrough.JSContact` | `passthrough_js` | **extended** · `JSPROP` — JSContact-only unknowns carried via the RFC 9555 JSPROP/JSPTR extension property | **unsupported** — no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) | **exact** · `(pointer keys)` — re-spliced verbatim at the recorded JSON pointers | **extended** · `JSPROP` — v3: unsupported |

## Canonical fields with no neutral-model home (issue #515)

These canonical `models.Contact` fields have no correspondence-table row: they
round-trip through the neutral Record (envelope fields) or have no neutral home at
all (CRM-local flags and relational timeline tables), and are therefore classified
**unsupported** in every format. The envelope fields are a *named* loss on file
export (`models.EnvelopeExportLossDiagnostics`); the flags and tables are deliberate
policy exclusions (ADR-0002 audit rule 4) and produce no loss report.

| Canonical field | Neutral home | vCard 4.0 | vCard 3.0 | JSContact | CardDAV (default v4) |
|---|---|---|---|---|---|
| `crm.gender` | `CRMEnvelope.Gender` | **unsupported** — free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary | **unsupported** — free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary | **unsupported** — free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary | **unsupported** — free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary |
| `crm.how_we_met` | `CRMEnvelope.HowWeMet` | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized |
| `crm.work_information` | `CRMEnvelope.WorkInformation` | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized |
| `crm.contact_information` | `CRMEnvelope.ContactInformation` | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized | **unsupported** — CRM-only envelope field; never serialized |
| `crm.circles` | `CRMEnvelope.Circles` | **unsupported** — legacy column superseded as a data source by circle_members (T2/T3); never serialized | **unsupported** — legacy column superseded as a data source by circle_members (T2/T3); never serialized | **unsupported** — legacy column superseded as a data source by circle_members (T2/T3); never serialized | **unsupported** — legacy column superseded as a data source by circle_members (T2/T3); never serialized |
| `crm.archived` | `none (Contact.Archived flat column)` | **unsupported** — CRM-local flag; deliberately never exported (ADR-0002 audit rule 4) | **unsupported** — CRM-local flag; deliberately never exported (ADR-0002 audit rule 4) | **unsupported** — CRM-local flag; deliberately never exported (ADR-0002 audit rule 4) | **unsupported** — CRM-local flag; deliberately never exported (ADR-0002 audit rule 4) |
| `crm.is_favorite` | `none (Contact.IsFavorite flat column)` | **unsupported** — CRM-local flag; deliberately never exported (issue #173) | **unsupported** — CRM-local flag; deliberately never exported (issue #173) | **unsupported** — CRM-local flag; deliberately never exported (issue #173) | **unsupported** — CRM-local flag; deliberately never exported (issue #173) |
| `crm.notes` | `none (relational timeline entity)` | **unsupported** — relational table keyed by contact, like Activities/Reminders; deliberately not projected into Card.Notes | **unsupported** — relational table keyed by contact, like Activities/Reminders; deliberately not projected into Card.Notes | **unsupported** — relational table keyed by contact, like Activities/Reminders; deliberately not projected into Card.Notes | **unsupported** — relational table keyed by contact, like Activities/Reminders; deliberately not projected into Card.Notes |
| `crm.activities` | `none (relational timeline entity)` | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record |
| `crm.reminders` | `none (relational timeline entity)` | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record | **unsupported** — separate relational entity, keyed by contact ID; never embedded in the single-contact Record |

## Loss reports (DATA-02 input)

Exactly the matrix's **unsupported** and **lossy** cells across the three serialized
formats, with the reason a DATA-02 runtime loss report must name. The correspondence
is bidirectional and asserted by `matrix_test.go` (every unsupported/lossy cell has a
report, and every report is an unsupported/lossy cell).

| Concept | Format | Bucket | Reason |
|---|---|---|---|
| `kind` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `created` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `language` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `name.surname2` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `name.generation` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `name.phonetic` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `adr` | vCard 3.0 | **lossy** | v3 ADR has only the 7 legacy fields; RFC 9553/9554 component kinds beyond those and the CC parameter are dropped with a warn |
| `anniversary.birth` | vCard 3.0 | **lossy** | v3 BDAY is date-only; time-of-day dropped (warns) |
| `anniversary.death` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `anniversary.place.birth` | vCard 4.0 | **lossy** | Address structure flattened to BIRTHPLACE TEXT/URI (RFC 6474 §2.1); structure lossy, warns |
| `anniversary.place.birth` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `anniversary.place.death` | vCard 4.0 | **lossy** | Address structure flattened to DEATHPLACE TEXT/URI (RFC 6474 §2.2); structure lossy, warns |
| `anniversary.place.death` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `gramgender` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `gramgender` | JSContact | **lossy** | JSContact speakToAs.grammaticalGender is scalar (RFC 9553 §2.2.4); multiple neutral entries collapse to the language-selected/first entry (warns) |
| `pronouns` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `expertise` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `hobby` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `interest` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `note` | vCard 3.0 | **lossy** | v3 NOTE drops the AUTHOR/AUTHOR-NAME/CREATED params (RFC 9554-only; warns) |
| `keywords` | JSContact | **lossy** | JSContact keywords is a boolean-set; duplicate keywords collapse (warns) |
| `directory` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `contacturi` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `lang` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `related` | vCard 3.0 | **lossy** | v3 has no RELATED property; target redirected to AGENT with a warn, relation TYPE tokens lost |
| `member` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `pt.jscontact` | vCard 3.0 | **unsupported** | no vCard 3.0 home; dropped with a warn diagnostic (ADR-0002 degradation policy) |
| `crm.gender` | vCard 4.0 | **unsupported** | free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary |
| `crm.gender` | vCard 3.0 | **unsupported** | free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary |
| `crm.gender` | JSContact | **unsupported** | free-text CRM concept, deliberately not vCard GENDER / JSContact speakToAs (docs/specs/rfc6350-baseline.md); the issue #515 canary |
| `crm.how_we_met` | vCard 4.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.how_we_met` | vCard 3.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.how_we_met` | JSContact | **unsupported** | CRM-only envelope field; never serialized |
| `crm.work_information` | vCard 4.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.work_information` | vCard 3.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.work_information` | JSContact | **unsupported** | CRM-only envelope field; never serialized |
| `crm.contact_information` | vCard 4.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.contact_information` | vCard 3.0 | **unsupported** | CRM-only envelope field; never serialized |
| `crm.contact_information` | JSContact | **unsupported** | CRM-only envelope field; never serialized |
| `crm.circles` | vCard 4.0 | **unsupported** | legacy column superseded as a data source by circle_members (T2/T3); never serialized |
| `crm.circles` | vCard 3.0 | **unsupported** | legacy column superseded as a data source by circle_members (T2/T3); never serialized |
| `crm.circles` | JSContact | **unsupported** | legacy column superseded as a data source by circle_members (T2/T3); never serialized |
