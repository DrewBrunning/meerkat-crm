---
title: Deprecation Register
nav_order: 21
---

# Deprecation register

**The machine-readable list of every deprecation on a covered surface**, per
the [deprecation policy](deprecation-policy.md) (MAINT-01, issue #490). A row
is added in the **same change** that deprecates the thing and adds its runtime
signal — never as a follow-up. A row moves to `status: removed` in the change
that removes the thing.

`cd backend && go run ./cmd/deprecations` validates this file on every backend
PR (the `Security-doc citations` job of `.github/workflows/unit-tests.yml`) and
fails the build on a malformed row, a window shorter than 90 days, an
`earliest-removal` that is not a strictly later minor than `deprecated-in`, a
`deprecated` row with no replacement, or a `removed` row whose removal predates
its own window. It prints an advisory when a `deprecated` row is past its
`not-before` and still not removed.

## Row format

One [GFM table](#register) row per deprecation, ten `|`-delimited fields:

| Field | Meaning |
|---|---|
| `id` | Stable kebab-case identifier, unique in this file. Referenced by the release notes and the tracking issue. |
| `surface` | One of `api`, `schema`, `config`, `cli`, `export`, `behavior` — the covered surface from the policy. |
| `summary` | One line: what is deprecated. |
| `deprecated-in` | The release that first shipped the deprecation notice — `vMAJOR.MINOR` or `vMAJOR.MINOR.PATCH`. |
| `deprecated-on` | The date that release shipped — `YYYY-MM-DD`. |
| `replacement` | What to use instead. Required — a deprecation with no replacement is a removal. |
| `earliest-removal` | The first release removal is *permitted* in — must be a strictly later **minor** than `deprecated-in`. |
| `not-before` | The first date removal is permitted — `YYYY-MM-DD`, at least 90 days after `deprecated-on`. |
| `status` | `deprecated` (in its window), `removed` (gone), or `withdrawn` (the deprecation was reversed; the thing stays supported). |
| `tracking` | Issue/PR reference. For `status: removed`, MUST also carry `removed-in=vX.Y[.Z] removed-on=YYYY-MM-DD`. |

The effective removal date is **whichever of `earliest-removal` and
`not-before` lands later** — at least one minor release *and* at least 90 days,
per the policy.

## Register

<!-- deprecations:begin -->

| id | surface | summary | deprecated-in | deprecated-on | replacement | earliest-removal | not-before | status | tracking |
|---|---|---|---|---|---|---|---|---|---|

<!-- deprecations:end -->

_No deprecations are currently in effect. The 2026-09-08 retroactive audit
([policy §Retroactive audit](deprecation-policy.md#retroactive-audit)) found
nothing on a covered surface deprecated in fact but not in writing._
