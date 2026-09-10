---
title: Issue Classification
nav_order: 23
---

# Issue classification

**This is the canonical statement of how work is classified (MAINT-03, issue
#492).** GitHub Issues is the only tracker this project has — there is no
separate backlog, roadmap doc, or planning board. That makes the labelling
scheme *the planning system*, and a planning system that lives only in people's
heads drifts. This page writes it down: what each label means, how priority and
milestone relate, how a bug's severity is recorded, when an issue is ready to
work, and what "closed" means.

The program index [#544](https://github.com/DrewBrunning/mycorrhizal-crm/issues/544)
records the *decisions* behind several of the rules here (pinned, with dates);
this page is where a contributor finds the rules themselves. A
breaking-change issue ([`breaking-change-policy.md`](breaking-change-policy.md),
MAINT-02) or a deprecation issue ([`deprecation-policy.md`](deprecation-policy.md),
MAINT-01) is labelled, milestoned, and — if it is a bug — severity-rated by
this page's rules, and the incident severity routing in
`docs/security/incident-response.md` (issue #509) keys off the severity ladder
below.

## Label taxonomy

Labels fall into groups. Within a group marked *exclusive*, an issue carries
exactly one.

### Type — *exclusive*

| Label | Meaning | Applied by |
|---|---|---|
| `bug` | Something that is supposed to work does not. Carries a `Severity:` line (below). | Reporter or triager |
| `enhancement` | New capability, or a change to how something already works. | Reporter or triager |

Every issue is one or the other. A "bug" that is really a missing feature is an
`enhancement`; an `enhancement` that documents current-behavior-is-wrong is a
`bug`.

### Area — *not exclusive*

| Label | Meaning | Applied by |
|---|---|---|
| `security` | Security review, hardening, or compliance work; anything touching auth, crypto, the SSRF guards, isolation, or a hostile-input parser. | Triager |
| `observability` | Logs, metrics, correlation IDs, the system-event timeline, health surface. | Triager |
| `docker` | The images, `docker-compose*.yml`, entrypoints, the published-image pipeline. | Triager or Dependabot |
| `documentation` | `docs/`, `README*`, `CLAUDE.md`, in-repo runbooks. | Triager |
| `dependencies` | A dependency-manifest change. | Dependabot |
| `go`, `javascript`, `github_actions` | Ecosystem tags on a dependency PR. | Dependabot |

Area labels are additive and optional — a change can be `security` **and**
`observability`, or carry none. They exist for filtering, not for routing.

### Priority — *exclusive*: `p0`–`p3` = commitment and scheduling horizon

**Priority expresses how firmly the project intends to do something, and how
soon — not which milestone it lands in.** The milestone is the actual plan;
priority is the trajectory toward one.

| Label | Name | Meaning |
|---|---|---|
| `p0` | Should be prioritized | A blocker for the milestone **currently in flight** — it has to be addressed before that milestone can close. |
| `p1` | Should be scheduled | Belongs in an **upcoming** milestone; the next planning pass should give it one. |
| `p2` | Should be implemented at some point | A decided commitment — we *will* do this — but not pressing enough to hold a near-term milestone. May sit with no milestone. |
| `p3` | Potential improvement | Recorded, **not yet committed to**. A candidate worth keeping in view, not a promise. |

Sequencing still comes from the milestone and the dependency order; priority
says which issues should be *acquiring* a milestone next. This refines the #544
note ("importance to `1.0.0`, decoupled from milestone", 2026-08-26): priority
stays decoupled from *which* milestone an issue lands in, but it does describe
the scheduling trajectory rather than a raw `1.0.0`-importance score. The
earlier drift the note reacted to — every `v0.6.4` issue `p0`, every `v0.6.11`
issue `p2`, the label saying nothing the milestone did not — is still what this
definition exists to prevent.

Not a bulk-relabel: an issue is moved to its correct priority when it is next
triaged.

### Workflow / meta — *not exclusive*

| Label | Meaning |
|---|---|
| `duplicate` | Closed in favor of another issue, which it links. |
| `wontfix` | A deliberate decision not to do this. The issue records why, then closes. |
| `question` | Needs information before it can be classified as `bug` or `enhancement`. |
| `good first issue` | Small, well-bounded, low-context — a good entry point. |

### Not a label

`wcag-aaa` appears in early planning notes but is **not** a live label.
Accessibility work (e.g. [#196](https://github.com/DrewBrunning/mycorrhizal-crm/issues/196))
rides `enhancement` + `documentation` as appropriate. Recorded here so the
absence is understood as intentional, not an oversight to "fix".

## Milestone convention

- **No milestone = not scheduled.** An issue with no milestone is not on the
  plan yet — it is a `p2` (a commitment still awaiting a slot) or a `p3` (not
  committed to). Every issue that *is* scheduled carries exactly one milestone;
  a `p0` or `p1` without one is a triage gap to close.
- **Milestone due dates are sequencing markers, not commitments.** They exist
  because GitHub sorts the milestones page by due date; the spacing is
  arbitrary and re-spaced freely (#544).
- **Every milestone has a gate issue.** It holds that milestone's acceptance
  criteria as a checklist, is labeled `p0`, and is closed **last** — after
  every other issue in the milestone. A criterion is checked only with a
  citation: a test name, a CI run, a document, or a `file:line`. The gate issue
  is how "the milestone is done" becomes a verifiable claim rather than a
  feeling. (Convention introduced by #544.)

## Bug severity

**Severity is a separate axis from priority** — priority is "how firmly and how
soon do we intend to do this", severity is "how bad is it when it happens". A
`bug` states its severity as a `Severity:` line in the issue body (not a label
— this project keeps the record in the issue body and does not grow the label
set for it):

| Severity | Definition | Response expectation |
|---|---|---|
| `sev1` | Data loss or corruption, a security or cross-user isolation breach, or auth bypass. | Triaged immediately; follows `docs/security/incident-response.md`. A live `sev1` blocks the next release. |
| `sev2` | Wrong data or a silent failure with **no workaround** — the user cannot tell it went wrong, or cannot avoid it. | Acknowledged within **5 business days** (the `SECURITY.md` number, reused rather than reinvented); fixed before the milestone it lands in closes. |
| `sev3` | Wrong or degraded behavior **with a workaround** the user can apply. | Scheduled like any other issue by priority + milestone. |
| `sev4` | Cosmetic, or a trivial inconvenience. | Batched; fixed opportunistically. |

Severity and priority are set independently. A `sev1` is not automatically
`p0` (a one-off already patched may be `p2` for follow-up hardening); a `sev4`
that blocks the milestone in flight is still `p0`.

## Ticket-readiness bar

An issue is **ready** when someone can implement it with no judgement calls
left — no "figure out which", no "decide whether", no unstated design
question. The standard, applied to every planned issue:

1. **Context** — why this matters and what the current state is, with
   `file:line` citations to the code in question.
2. **Recommended actions** — the concrete change, specific enough to execute.
   Where a real choice exists, the issue makes it (or states the two options
   and that either is acceptable), rather than leaving it to the implementer.
3. **How to verify** — the observable check that says it is done: a test to
   write, a command whose output changes, a doc that must now say X.

Grep your own draft for hedges — "verify before", "confirm which", "figure
out", "TBD" — and resolve each one before the issue is called ready. The
security batch [#368–#420](https://github.com/DrewBrunning/mycorrhizal-crm/issues/368)
is the reference example of the bar met.

## Issue lifecycle

| State | How you can tell |
|---|---|
| **Unplanned** | Open, no milestone. Not committed to; may sit indefinitely. |
| **Planned** | Open, has a milestone, meets the readiness bar. |
| **In progress** | A branch exists (`feature/<thing>` or `claude/<thing>`), usually with a draft PR. |
| **Landed / closed** | The implementing PR is merged **and** the change is verified. |

**"Closed" means the ticket landed** — the PR merged and the verification in
"How to verify" passed — not that a PR was opened, and not that the work was
abandoned (that is `wontfix` or `duplicate`, with a reason). There is no
separate "done" column: the issue body plus the commit history *is* the status
record, so a closed issue should read as a complete account of what was done.
The milestone's gate issue closes after everything else in the milestone.

## Conformance

Every open issue conforms to this page, or is corrected to conform the next
time it is triaged. This page is the reference such a correction points at — a
relabel or a milestone change cites "MAINT-03" the way a security change cites
an ASVS row.
