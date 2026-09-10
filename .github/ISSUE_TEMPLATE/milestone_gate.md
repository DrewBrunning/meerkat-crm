---
name: Milestone / release gate
about: The exit gate for a milestone — its acceptance criteria as citable checkboxes
title: 'vX.Y.Z — Milestone gate (verify acceptance criteria)'
labels: ''
assignees: ''

---

<!--
The gate issue is the LAST thing closed in its milestone. It exists so the
milestone's acceptance criteria are verified deliberately rather than assumed
once the final feature ticket merges.

Two rules make it worth having, and both are easy to erode:

  1. A box is checked only with a citation — a test name, a CI run, a document,
     or a `file:line`. "We did that work" is not evidence that the criterion
     holds; it is a memory of having intended it to.
  2. A criterion that cannot be met is either descoped by editing the milestone
     description (with a note here saying why) or filed as a follow-up issue
     with an explicit disposition. Never left silently unchecked.

Replace the milestone-specific criteria below; keep the standing ones.
-->

## Deliverable

The exit gate for milestone `vX.Y.Z`. This issue is the **last thing closed** in the milestone: it holds that milestone's acceptance criteria as checkboxes so they are verified deliberately rather than assumed once the last feature ticket merges.

> One-sentence statement of what this milestone establishes, copied from the milestone description.

Check a box only with a citation — a test, a CI run, a document, or a `file:line`. "We did that work" is not evidence that the criterion holds.

## Acceptance criteria

- [ ] (milestone-specific criterion)
- [ ] (milestone-specific criterion)

### Standing criteria

Carried by every gate. Do not delete them when adapting this template — if one
genuinely does not apply to a milestone, say so here rather than dropping it.

> **Security-doc citations are no longer a per-milestone checkbox.** As of issue #608 the
> `citecheck` gate is enforced in three places with no human in the loop: the per-PR
> `Security-doc citations` job, the `release_gate: true` poll on the release commit, and a
> direct `go run ./cmd/citecheck` hard step in the REL-06 release workflow (`release.yml`).
> The obligation has one home — the release process — so this gate does not restate it. See
> `docs/security/asvs-l2-verification-report.md` §8–§9.

- [ ] No new *class* of security-relevant surface went unrecorded — if this milestone added
      one (a new client, a new outbound integration, a new persistence target, a new
      authentication path), §9 of `docs/security/asvs-l2-verification-report.md` has a row
      for it naming either the mechanism that fails when the next instance is added, or the
      issue that will build one. If the milestone added no new class, say so and check the
      box. (Standing criterion, issue #378. This is deliberately a question for whoever
      closes the gate — they know what the milestone contained — rather than a scheduled
      audit, which fires on a calendar and lands on someone without that context.)

<!--
RELEASE gates only (0.8.0 / 0.9.0 / 1.0.0, and any future shipping milestone)
also carry the full re-verification criterion. A per-milestone gate does not —
re-running the whole ASVS pass every milestone is disproportionate, while
letting a released claim go unverified for a year is not. Uncomment for a gate
that actually ships something:

- [ ] The ASVS L2 / MASVS-L1 claim has been **re-verified against the shipped code**, not
      inherited from an earlier milestone — cite a dated row in
      `docs/security/asvs-l2-verification-report.md`'s changelog. §8 of that report is the
      procedure: steps 1–2 are automated, and the four manual audits each produced
      single-digit candidate lists on the first pass, so a re-pass is about an hour rather
      than a rebuild. (Standing criterion, issue #378.)
-->

## Verify

Every box above is checked and carries a citation. Any criterion that cannot be met is either descoped by editing the milestone description (with a note here saying why) or filed as a follow-up issue with an explicit disposition — never left silently unchecked.

## Notes

Milestone `vX.Y.Z`. Part of the `0.6.x` → `1.0.0` hardening program; see the program index issue. Companion gates: #500 (`0.8.0`), #503 (`0.9.0`), #525 (`1.0.0`).
