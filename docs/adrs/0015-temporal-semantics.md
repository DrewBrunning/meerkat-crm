# ADR 0015: Temporal semantics — instants, calendar dates, partial dates, and the one wall clock

- **Status:** accepted
- **Date:** 2026-09-07
- **Implements:** issue #482 (DATE-01). The `v0.6.11` criterion covered here: every date-ish field in the
  model is classified into exactly one temporal category, and the wall-clock / leap-day / partial-date /
  zone-less rules are written down concretely enough to write a test from.
- **Feeds:** #483 (DATE-02, pathological-date tests — writes the tests for each rule stated here),
  #541 (the `v0.6.11` milestone gate). Related: #526 (scheduled-job catch-up — its de-duplication and the
  DST rules below must agree), #484 (I18N fixtures), #438 (migration semantics for date fields), #515
  (canonical-field round-tripping).
- **Depends on:** ADR 0011 (scheduled-job catch-up — the missed-occurrence recovery half of the DST rule),
  ADR 0002 (correspondence table — birthday/anniversary mapping to the neutral model).

## Context

Time in this product means several different things that all get stored as "a date", and the
distinctions were convention rather than type:

- **`Birthday` is a `string`** (`backend/models/contact.go:90,96`), not a `time.Time`, with a custom
  `birthday` validator. That is defensible — vCard BDAY permits a year-less `--MM-DD` that no timestamp
  type can represent — but the semantics lived entirely in the validator and in convention. `Anniversary`
  followed the same shape.
- **Reminders fire on one server-wide clock**: `REMINDER_TIME` (default `06:00`) in `REMINDER_TIMEZONE`
  (default `UTC`), IANA-validated (`backend/config/config.go:50-51,213-214,662-671,855-868`), scheduled
  via `gocron.NewScheduler(cfg.GetReminderLocation())` with the daily job registered against it
  (`backend/main.go:284,306-307`). There is no per-user
  timezone, and that real product decision was written nowhere an operator would find it.
- **Audit and record timestamps** are `time.Time`, generated UTC.
- **Imported timestamps** may arrive with no timezone at all.
- The two load-bearing edge behaviours — **what happens to a wall-clock reminder time at a DST
  transition**, and **what the next occurrence of 29 February is** — were not specified anywhere. The
  code did things (Go `time.Date` day-overflow and DST normalization) but differently in different
  places: birthdays/life-events/CalDAV advanced a year-less 29-Feb to 1 March, while the recurring
  reminder engine clamped a 29-Feb `yearly` reminder to 28 February.

The failure mode is quiet: a birthday reminder on the wrong day, an "on this day" that is off by one, a
29-Feb birthday that is never reported as "today". Nobody files a bug for a birthday reminder arriving a
day early; they just trust it less.

## Decision

### The five temporal categories

Every date-ish value in the product is exactly one of these:

1. **Instant** — a point on the UTC timeline. Carries an offset or `Z` on the wire, is never
   zone-converted for storage, and is rendered in whatever zone the client prefers. Stored as Go
   `time.Time`. Server-generated instants are UTC by construction; a client-supplied RFC3339 instant is
   unambiguous because an offset is present. SQLite stores the driver's offset-bearing text serialization,
   so **write UTC** (`Z` or `+00:00`) at every instant boundary — the recurring-reminder arithmetic does
   this deliberately (`CalculateNextReminderTime`, `backend/services/reminder_service.go:562-600`) — and a
   value written with a non-UTC offset is a latent SQL-comparison hazard, not a feature.
2. **Date-only** — a whole calendar date with no time of day and no zone. `2026-03-14` is the 14th
   everywhere; it is never converted across zones. A birthday or anniversary is this.
3. **Partial date** — a calendar date with one or more components deliberately unknown: month/day with no
   year (`--MM-DD`, a leap-day birthday), year with no month/day (a life event known only to `1995`),
   year-month, month-only, or day-only. Explicitly supported for RFC 6350/9553 compatibility. Partial
   dates have **no total order** and no annual recurrence unless they carry a month *and* a day (see the
   rules below).
4. **Local wall time** — an `HH:MM` label with no date, meaningful only inside a named IANA zone.
   Exactly one such value exists in the whole product: the operator's reminder run time
   (`REMINDER_TIME` + `REMINDER_TIMEZONE`).
5. **Zone-less imported timestamp** — a timestamp that arrived without an offset or zone suffix. Handled
   by stated assumption, never silently.

Categories 2 and 3 are the same *kind* of value — a calendar date with no time and no zone — differing
only in precision. They are split here because their behavioural rules (sorting, next occurrence,
age) differ.

### Classification of the model's date-ish fields

Every field below belongs to exactly one category. "May hold a partial value" marks a date-only surface
whose storage also accepts a reduced-precision shape.

#### Category 1 — instant (`time.Time`, UTC)

**Row bookkeeping on every table**: `created_at` / `updated_at` (and `deleted_at` for soft-deleting
entities) are instants on every entity that carries them. `gorm.Model` supplies them to `Contact`,
`Note`, `Activity`, `Reminder`, `ReminderCompletion`, `User`, `ApiToken`, `DeviceGrant`, `Webhook`,
`WebhookDelivery`, `NotificationConfig`, `Attachment`, `ContactSubscription`, `CalendarSubscription`,
`ImmichConfig`, `WebdavConfig`, `PaperlessConfig`, `SeafileConfig`, and the UUID-PK entities
(`Circle`, `CircleMember`, `Tag`, `ContactTag`, `Household`, `HouseholdMember`,
`DismissedHouseholdSuggestion`, `LifeEvent`, `Gift`, `ConversationAgenda`, `Preference`, `CadencePolicy`,
`RelationshipEdge`, `Duplicate`, `FieldDefinition`, `FieldValue`, `LinkFieldType`,
`ContactSyncConflict`, `ContactShare`, `ContactSyncLink`, `CalendarEventLink`, `ExternalIdentity`,
`ExternalActivity`, `ReachOutSuggestion`, `ReachOutCursor`, `AlertState`) declare them explicitly. The
audit trail (`AuditEvent.CreatedAt`, `backend/models/audit.go`) and the operational timeline
(`SystemEvent.CreatedAt`, `JobRun.CreatedAt`, `ImportRun.CreatedAt`) are instants too; `AuditEvent`
truncates to microseconds for deterministic chain hashing (`audit.go`).

**Domain-meaningful instants** (the ones whose time-of-day means something):

| Field | Where | Notes |
|---|---|---|
| `Activity.Date`, `Note.Date` | `backend/models/activity.go`, `note.go` | When the interaction / note happened. Full instant on the wire; the UI treats it as a date-time of the interaction |
| `Gift.Date` | `backend/models/gift.go` | Nullable — an idea has no date |
| `Reminder.RemindAt`, `.LastSent`; `ReminderCompletion.CompletedAt` | `backend/models/reminder.go` | Day-granular in intent: the write path zeroes the time-of-day (`reminder_controller.go:49-58`) and the daily job compares against a day boundary |
| `ExternalActivity.OccurredAt` | `backend/models/external_activity.go` | Timeline sort key for an external system's event |
| `ConversationAgenda.DiscussedAt` | `backend/models/conversation_agenda.go` | Nil = open item |
| `SystemEvent.OccurredAt` | `backend/models/system_event.go` | Can be backdated vs `CreatedAt` |
| `JobRun.StartedAt/FinishedAt`, `JobExecution.LastRunAt/LockedAt` | `backend/models/job_run.go`, `job_execution.go` | The catch-up / de-dup clocks ADR 0011 keys on |
| `StorageSample.TakenAt`, `OperationalCheckResult.CheckedAt` | `backend/models/storage_sample.go`, `operational_check_result.go` | Measurement / self-check time |
| Sync-health clocks `LastAttemptAt/LastSuccessAt/LastFailureAt/IncidentFirstFailureAt/TerminalFailureAt` | `backend/models/sync_health.go` | Per-subscription sync state (ADR 0005 / INT-02) |
| `NotificationDelivery.SentAt`, `WebhookDelivery.NextRetryAt`, `ContactShare.RespondedAt`, `CardDAVSync.LastModified` | `backend/models/notification.go`, `webhook.go`, `contact_share.go`, `carddav.go` | Delivery / retry / share / sync-token clocks |
| `User` password-reset and TOTP clocks; `ApiToken`/`DeviceGrant` `LastUsedAt/RevokedAt/ExpiresAt` | `backend/models/user.go`, `api_token.go`, `device_grant.go` | Lifecycle instants |

`ImmichConfig.LastSyncedAt` / `ExternalIdentity.LastSyncedAt` and the legacy subscription `LastSyncedAt`
fields are instants of the same kind (pre-`SyncHealthFields` pass/fail clocks).

#### Category 2/3 — calendar dates (date-only, no time, no zone; some surfaces may hold partial values)

| Field | Where | Precision |
|---|---|---|
| `Contact.Birthday`, `Contact.Anniversary` | `backend/models/contact.go:90-96,137-139` | `YYYY-MM-DD` **or** year-less `--MM-DD` (partial). String by design (vCard BDAY grammar); validated by the `birthday` validator |
| `ContactSummary.Birthday`, `Birthday` DTO, `BriefingUpcomingDate.Date` | `backend/models/contact_summary.go`, `dtos.go`, `briefing.go` | Mirrors of the flat columns; same forms |
| `LifeEvent.Date` | `backend/models/life_event.go:141` | `contactmodel.PartialDate` (any of year/month/day). The neutral model's calendar-date type; no zone, only `calendarScale` |
| `HouseholdMember.Since/Until` | `backend/models/household.go:105-106` | `PartialDate`-compatible string, unvalidated |
| Neutral `Card.Anniversaries[].Date` (JSContact) | `backend/contactmodel/model.go` | `AnniversaryDate{PartialDate | Timestamp}` — the discriminated union that keeps partial and timestamp distinct through the neutral model (ADR 0002) |

**The one REST/relational format set** for date-only/partial values is exactly two shapes — full
`YYYY-MM-DD` and year-less `--MM-DD` — enforced by the `birthday` validator
(`backend/middleware/validation.go:74,196-221`), mirrored by hand in `birthdayFormatRE` /
`parsePartialDateString` (`backend/models/contact_record.go:734-790`) and `IsValidBirthdayFormat`
(`backend/services/import_service.go`), and matching the richer partial grammar only at the vCard /
JSContact boundary, where RFC 6350 forms such as `YYYY`, `YYYY-MM`, `--MM`, `---DD`, compact
`YYYYMMDD`/`--MMDD`, and timestamped BDAY values are parsed into the neutral `PartialDate`/`Timestamp`
shapes (`backend/vcard3/adapter.go`, `backend/vcard4/adapter.go`, `backend/jscontact/`). The validator is
**lexical**: it does not range-check months or days, by design (a reduced-precision miss like `1990-99-99`
is not a valid partial date either, and DATE-02 owns pathological values). Legacy flat rows that do not
match the two canonical shapes are left untouched on the scalar rather than corrupted on round-trip
(`contact_record.go:734-750`).

#### Category 4 — local wall time

`Config.ReminderTime` + `Config.ReminderTimezone` (`backend/config/config.go:44-53,213-214,662-671`,
`GetReminderLocation` at `855-868`). The only wall-clock value in the product, and it is operator
configuration, not a stored field. The scheduled digest and the life-event reminder hour (fixed at 09:00,
`backend/controllers/life_event_controller.go:19-22`) both interpret it in the reminder zone.

#### Category 5 — zone-less imported timestamps

The import plane's `parseSourceTime` (`backend/services/import_source.go:699-721`) accepts RFC3339 plus
two naive layouts (`2006-01-02 15:04:05`, `2006-01-02`); the two naive layouts are parsed with
`time.Parse`, i.e. **assumed UTC**. That assumption is a stated assumption about each source system's
writer, not a verified fact (see Rule 5). Raw vCard DATE-AND-OR-TIME strings without a zone are preserved
verbatim on the neutral plane rather than coerced (see the `sem-timestamp-no-tz` adversarial fixture,
`docs/adversarial-fixtures/MANIFEST.md`).

### Rule 1 — instants are UTC and unambiguous

Server-generated instants are `time.Now().UTC()`; audit/hash and system-event writers pin this explicitly
(`backend/models/audit.go`, `system_event.go`). Wire format is RFC3339; a client may send an offset and
the instant is still unambiguous. **Store UTC** — a `time.Time` carrying a fixed non-UTC offset is the
one DB-level hazard (SQLite compares the offset-bearing text), so writers that normalize to UTC
(`CalculateNextReminderTime`) are the pattern, not an optimization.

### Rule 2 — date-only / partial values are never zone-converted

A stored `YYYY-MM-DD` or `--MM-DD` (or a `PartialDate`) has no time and no zone and is never shifted for
display, sort, or comparison. The *only* zone-aware step is deciding which calendar day is "today" (Rule
4's clock), and that step operates on the wall calendar, not on the stored value. vCard/JSContact export
preserves the calendar date verbatim. (The one deliberate zone nuance: iCalendar all-day VEVENTs are
served as `VALUE=DATE` so clients never timezone-shift them — `backend/caldav/backend.go:204-214`.)

### Rule 3 — partial dates: sorting, next occurrence, and age

- **No total order.** A partial date is not comparable to an instant or to another partial date of
  different precision. Surfaces that must sort or window them resolve each partial to a concrete UTC
  instant with an explicit, documented fallback ladder — the timeline's `timelineLifeEventDate`
  (`backend/controllers/timeline_controller.go:453-473`), mirrored by the web client: full date → UTC
  midnight of that date; month/day only → the *current* year's UTC midnight; year-only → 1 January UTC;
  nothing usable → `CreatedAt`. CalDAV windowing uses the same per-year re-resolution
  (`eventInWindow`, `backend/caldav/backend.go:375-396`).
- **Next occurrence** is defined only for a partial date carrying a **month and a day**: it recurs
  annually. The next occurrence is the first such date not before "today" (Rule 4's calendar day); a
  month/day in the past wraps forward a year — the Dec 31 → Jan 1 wrap is pinned by
  `DaysUntilBirthday` (`backend/services/birthday_service.go:128`). A **year-only** partial (a life
  event known only to a year) has no annual occurrence: it is not a calendar event and cannot generate a
  reminder (`lifeEventHasCalendarDate`, `backend/controllers/life_event_controller.go:57-71`;
  `backend/caldav/backend.go:232-237`).
- **Age** is computable only from a full `YYYY-MM-DD` value; a year-less partial never enters an age
  calculation (there is no age computation anywhere in the product today — this is the convention a
  future one must use). Under Rule 6 a 29-Feb birthdate ages up on its occurrence date.

### Rule 4 — the single clock: reminders and "today" use the operator's zone, server-wide

`REMINDER_TIME`/`REMINDER_TIMEZONE` are **one server-wide clock**: every channel, every user, every
reminder on the deployment is scheduled against them. There is no per-user timezone today; a multi-zone
deployment should expect all of its users to share the operator's chosen zone, and per-user zones are a
future feature, not current behaviour. Concretely:

- The daily digest job runs once per day at `REMINDER_TIME` in the reminder zone
  (`backend/main.go:306-307`), and its "today" day boundary — which reminders are due, and which
  birthdays fall *today* — is `23:59:59` of the current local day **in the reminder zone**
  (`backend/services/reminder_service.go:91-98`, birthday fetch at `470-471`). Birthdays reach the digest
  only when `DaysUntilBirthday(...) == 0` against that zone's "now" (`reminder_service.go:133-152`).
- Life-event reminders fire at a fixed 09:00 in the **same** zone (`nextRemindAt`,
  `backend/controllers/life_event_controller.go:73-81`).
- **Known divergence, written down rather than rounded away:** some interactive endpoints compute
  "today" with server-local `time.Now()` rather than the reminder zone — `GET /contacts/birthdays`
  (`backend/controllers/contact_controller.go`), the dashboard composite, and `GET /reminders/upcoming`
  (`backend/controllers/reminder_controller.go`) — while the scheduled digest, cadence, timeline, and
  briefing paths use the reminder zone. On a host whose local zone differs from `REMINDER_TIMEZONE`, the
  UI can disagree with the email by a day. Aligning every interactive endpoint to `GetReminderLocation`
  is a follow-up (the endpoints take `time.Now()` where the digest passes the zone-aware value); DATE-02
  or a dedicated temporal-conformance ticket should pin the aligned behaviour.

### Rule 5 — DST and the wall clock: one run per local label, never skipped, never doubled

The daily occurrence is defined by a **wall-clock label** (`HH:MM`) in the reminder zone, not by a UTC
instant. gocron v1.37.0 (pinned in `backend/go.mod`) reconstructs each day's fire time in the scheduler
location via Go `time.Date` (`roundToMidnightAndAddDSTAware`), so:

- **Spring forward (gap)** — a label that does not exist that day (e.g. `02:30` in `America/New_York`)
  fires **once**, normalized to the first valid instant that carries the label after the transition —
  Go resolves the non-existent wall time to the post-transition offset, so the fire instant is the same
  as every subsequent day's under the new offset. The run still happens; it is not skipped and not
  doubled.
- **Fall back (fold)** — an ambiguous label fires **once**, at the **earlier** of the two passes through
  the hour (Go resolves the ambiguity to the pre-fall-back offset). It never fires twice.
- **A wholly missed fire day** (process down at the fire instant) is not replayed by gocron; the paired
  boot-time `Initial` trigger beside every job plus `acquireJobLock` implement ADR 0011's
  catch-up-with-de-duplication (one logical occurrence per period — `backend/main.go:306-307`,
  `backend/services/reminder_service.go:44`, `backend/services/job_lock.go`). This ADR's
  "never skipped, never doubled" statement and ADR 0011's de-duplication are the same policy seen from
  two sides; issue #526's ticket text and this rule agree.
- These gap/fold behaviours are inherited from the pinned gocron + Go `time.Date`; they are stated here
  so DATE-02 can pin them with tests. The days-until arithmetic that truncates absolute hours
  (`DaysUntilBirthday`) is *not* DST-safe across a spring-forward (two local midnights are 23 absolute
  hours apart) — the cadence engine's `calendarDaysBetween` rounds for exactly that reason
  (`backend/services/cadence_service.go:37-52`); hardening `DaysUntilBirthday` is DATE-02 material.

### Rule 6 — 29 February advances to 1 March in a non-leap year

There is no answer for 29 February that is right for everyone, so this ADR picks one and pins it: **a
29-Feb calendar date occurs on 1 March in a non-leap year** (the "advance to the next real calendar
day" rule — Go `time.Date` day-overflow implements it natively, which is why the code converges on it).
Consequences:

- **Birthdays**: a stored 29-Feb birthday is celebrated on 1 March in non-leap years. `DaysUntilBirthday`
  reports 0 ("today") on 1 March of a non-leap year (`backend/services/birthday_service.go:128`), and
  `GetUpcomingBirthdays` fetches the row on that one date so the digest and the birthdays list can report
  it — its stored month (02) is not in the query window in March, so the preselect has an explicit
  leap-day branch for `1 March of a non-leap year` (`birthday_service.go:30-42`).
- **Life events and reminders**: `nextRemindAt` and yearly reminder recurrence land on 1 March too
  (`addYears` is a plain `AddDate`, which day-overflows 29 Feb → 1 Mar; `backend/services/
  reminder_service.go:549-558`). The single consumer that used to *clamp* to 28 February — yearly
  recurring reminders — was aligned to this rule in DATE-01 (see Consequences).
- **Monthly cadence is deliberately different.** A monthly recurrence clamps to the last real day of the
  target month (`addMonths`, Jan 31 → Feb 28/29) — that is the "monthly on the Nth" reading, not an
  annual date, and is unchanged (`reminder_service.go:527-547`).
- **CalDAV caveat, stated rather than hidden:** a year-known 29-Feb life event is served as a
  `FREQ=YEARLY` VEVENT anchored on its real date. Per RFC 5545 a yearly recurrence of 29 Feb simply has
  no instance in a non-leap year, so spec-conformant calendar clients show it only in leap years —
  regardless of this product rule. The app's own surfaces (digest, briefing, birthdays list) use Rule 6;
  materializing per-year overridden instances for CalDAV is out of scope here.

### Rule 7 — zone-less imported timestamps are UTC, by stated assumption

A timestamp without an offset or zone on the import plane is read as **UTC** (`parseSourceTime`,
`backend/services/import_source.go:699-721`). This is an assumption about the source, applied and
written down here; if a source system ever turns out to have written wall time in its own zone, the fix
is per-source metadata, not a global reinterpretation.

## Code ↔ document citations

The validators and the document cite each other so there is no second, parallel truth about what a
birthday may be:

- `validateBirthday`'s doc comment names this ADR and its exact accepted set (`backend/middleware/
  validation.go:196-221`); `birthdayFormatRE`'s mirror comment names it too (`backend/models/
  contact_record.go:734`); `Contact.Birthday`/`Anniversary`, `LifeEvent.Date`, and
  `HouseholdMember.Since/Until` doc comments classify themselves (`backend/models/contact.go:90-96,
  137-139`, `life_event.go:141`, `household.go:105-106`).
- The config struct, `GetReminderLocation`, and the scheduler carry the "one server-wide clock" and DST
  statements (`backend/config/config.go:44-53,855-868`, `backend/main.go:284,306-307`); the import parser
  carries the zone-less assumption (`backend/services/import_source.go:699-724`).
- DATE-02 (#483) writes the pathological-date tests against the rules above.

## Consequences

- **One behavioural change shipped with this decision:** yearly recurring reminders from a 29-Feb base
  date now advance to **1 March** in a non-leap year instead of clamping to 28 February
  (`addYears`, `reminder_service.go:549-558`; tests updated in `reminder_service_test.go`). The digest
  "Feb-29 birthday is never today in a non-leap year" gap is closed (`GetUpcomingBirthdays` leap branch,
  pinned by `birthday_service_test.go`).
- Operators now have the single-timezone statement where they configure it: `docs/notifications.md`,
  `docs/getting-started.md`'s environment table, and the two `.env.example` files point at this ADR.
- DATE-02 inherits a spec to test: partial-date resolution ladders, DST gap/fold pinning for the daily
  job, DST-safe days-until, the Feb-29 fetch window, and the interactive-endpoint zone divergence.
- No schema change; no migration. Category classification is documentation plus comment-level citations.
