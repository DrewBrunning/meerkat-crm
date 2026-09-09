package integrations

import (
	"fmt"
	"strings"
	"time"
)

// MatrixDocRelPath is the committed artifact this package projects to,
// relative to the repository root.
const MatrixDocRelPath = "docs/int-01-integration-classification-matrix.md"

// Render returns the full Markdown of the INT-01 matrix. The generator writes
// it to MatrixDocRelPath; the drift test fails until the committed file matches.
func Render() string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString("title: Integration Classification Matrix\n")
	b.WriteString("nav_order: 15\n")
	b.WriteString("---\n\n")

	b.WriteString("# INT-01 — Integration classification matrix\n\n")

	b.WriteString("> **Generated artifact — do not hand-edit.** The source of truth is\n")
	b.WriteString("> `backend/integrations` (`Registry()` + `Dispositions()` + `OutboundOperations()`). Regenerate with\n")
	b.WriteString("> `cd backend && go run ./cmd/genintegrationmatrix` (or `make gen-integration-matrix`);\n")
	b.WriteString("> the drift test `backend/integrations/matrix_test.go` fails until this file and the\n")
	b.WriteString("> registry agree, and `TestEveryOutboundClientIsClassified` fails if a new outbound\n")
	b.WriteString("> client is added under `backend/services/` without a row here.\n\n")

	b.WriteString("This product talks to a lot of software it does not control. Treating those as one\n")
	b.WriteString("category is how failure handling goes wrong: a CalDAV server being unreachable is a\n")
	b.WriteString("temporary degradation of a background sync; an OIDC provider being unreachable means\n")
	b.WriteString("nobody can log in; a Paperless instance being permanently gone means a stored\n")
	b.WriteString("reference never resolves again. Same word, three different correct behaviors. This\n")
	b.WriteString("matrix writes the classification down once so every client's retry, timeout, and\n")
	b.WriteString("surfacing decision is made against a shared position. DOC-03 (issue #488) publishes\n")
	b.WriteString("the operator-facing half and cites this document for the engineering half.\n\n")

	writeAxisLegend(&b)
	writeDispositionTable(&b)
	writeOutboundOperations(&b)
	writeSummaryTable(&b)
	writeSSRFSection(&b)
	writePerIntegration(&b)
	writeAddingSection(&b)
	writeRelated(&b)

	return b.String()
}

func writeAxisLegend(b *strings.Builder) {
	b.WriteString("## Classification axes\n\n")
	b.WriteString("Every integration is placed on five axes that determine what \"handle the failure\n")
	b.WriteString("correctly\" means for it.\n\n")

	b.WriteString("| Axis | Values | What it decides |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| **Criticality** | `required-when-enabled` · `optional` | Whether losing it blocks a whole workflow or just degrades a feature. |\n")
	b.WriteString("| **Direction** | `outbound` · `inbound` · `bidirectional` | Who opens the connection — and therefore who imposes the timeout. |\n")
	b.WriteString("| **Cadence** | `interactive` · `scheduled` · `event-driven` | Whether a failure is seen by a user immediately or only by a background job. |\n")
	b.WriteString("| **Data authority** | `remote-authoritative` · `shared` · `enrichment` · `none` | Whether the remote holds data we cannot reconstruct. |\n")
	b.WriteString("| **Failure impact** | `degraded-feature` · `blocked-workflow` · `silent-staleness` | What actually goes wrong. `silent-staleness` is the dangerous one — nothing looks wrong. |\n\n")
}

func writeDispositionTable(b *strings.Builder) {
	b.WriteString("## Transient vs permanent — the per-error classification\n\n")
	b.WriteString("The distinction #465 (INT-02), #466 (INT-03) and #467 (INT-04) depend on is a\n")
	b.WriteString("property of the **error**, not of the integration: a 503 is transient no matter who\n")
	b.WriteString("returns it; a 401 after a revoked token is permanent until a human acts. This table\n")
	b.WriteString("is `integrations.Dispositions()` — those tickets assert against it and must not carry\n")
	b.WriteString("a second copy of the judgment.\n\n")

	b.WriteString("| Failure mode | Class | Retry safe? | Honor `Retry-After`? | Rationale |\n")
	b.WriteString("|---|---|---|---|---|\n")
	d := Dispositions()
	for _, m := range FailureModes {
		x := d[m]
		b.WriteString(fmt.Sprintf("| `%s` | **%s** | %s | %s | %s |\n",
			m, x.Persistence, yesNo(x.Retryable), yesNo(x.HonorRetryAfter), x.Rationale))
	}
	b.WriteString("\n")
	b.WriteString("\"Retry safe\" means a retry is *sound* (idempotent or protected), not that the code\n")
	b.WriteString("retries automatically today — several integrations rely on the next scheduled run\n")
	b.WriteString("instead of an in-call loop. `permanent-until-human` failures must stop retrying and\n")
	b.WriteString("be surfaced (#467); the transition into and out of that state, and staleness\n")
	b.WriteString("tracking (\"last successful sync: 47 days ago\"), are #467/#427.\n\n")
}

func writeOutboundOperations(b *strings.Builder) {
	b.WriteString("## Outbound operations — retry safety\n\n")
	b.WriteString("The table above classifies *failure modes*. This one classifies the *operations*:\n")
	b.WriteString("every write or side-effecting call this app makes to an external system, by what\n")
	b.WriteString("happens on an **ambiguous failure** — the request left, the response never came\n")
	b.WriteString("back, and a retry might double the effect. This is `integrations.OutboundOperations()`;\n")
	b.WriteString("INT-03 (#466) asserts every such operation has a row, an idempotency class, and a\n")
	b.WriteString("named safeguard, and that a new outbound-write integration cannot be added without\n")
	b.WriteString("one. Read-only calls (Ping, discovery, HIBP, update-check) are not listed — there\n")
	b.WriteString("is nothing to double.\n\n")

	b.WriteString("| Operation | Integration | Idempotency | Safeguard on retry | Retry budget |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, op := range OutboundOperations() {
		b.WriteString(fmt.Sprintf("| `%s` | [`%s`](#%s) | `%s` | %s | %s |\n",
			op.ID, op.Integration, op.Integration, op.Class, op.Safeguard, op.RetryPolicy))
	}
	b.WriteString("\n")
	b.WriteString("`naturally-idempotent` means a bare retry converges; `conditionally-idempotent`\n")
	b.WriteString("means it is safe only with the stated precondition (`If-Match`, a dedup key);\n")
	b.WriteString("`not-idempotent` means a retry is only safe because a local delivery record keyed\n")
	b.WriteString("by the logical event recognizes it as a retry. `DispositionForHTTPStatus` and\n")
	b.WriteString("`RetryPolicy` (`backend/integrations/retry.go`) are the shared primitives every\n")
	b.WriteString("loop uses so backoff, jitter, `Retry-After`, and never-retry-a-permanent-status are\n")
	b.WriteString("decided in one place.\n\n")
}

func writeSummaryTable(b *strings.Builder) {
	b.WriteString("## The integrations\n\n")
	b.WriteString("| Integration | Criticality | Direction | Cadence | Data authority | Failure impact | Timeout | SSRF | Retry budget |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, in := range Registry() {
		b.WriteString(fmt.Sprintf("| [%s](#%s) | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			in.Name, in.ID,
			in.Criticality, in.Direction, in.Cadence, in.DataAuthority, in.FailureImpact,
			timeoutCell(in.Timeout), in.SSRF, oneLine(in.RetryBudget)))
	}
	b.WriteString("\n")
}

func writeSSRFSection(b *strings.Builder) {
	b.WriteString("## SSRF is a property of this whole surface\n\n")
	b.WriteString("Nearly every integration here takes a user- or operator-supplied URL, so it inherits\n")
	b.WriteString("the SSRF constraint. The guard is `httputil.SafeDialContext`: it re-resolves the\n")
	b.WriteString("host and pins a public address **at dial time**, so a redirect to an internal\n")
	b.WriteString("address and DNS rebinding both fail — a pre-flight URL check alone does not. Any\n")
	b.WriteString("new client inherits this requirement; `TestSSRFClaimsMatchSource` checks that a row\n")
	b.WriteString("claiming a guarded posture actually references `SafeDialContext` in its source.\n\n")

	b.WriteString("| Posture | Meaning | Integrations |\n")
	b.WriteString("|---|---|---|\n")
	for _, p := range []SSRFPosture{SSRFGuardedAlways, SSRFGuardedWhenEnabled, SSRFFixedEndpoint, SSRFUnguarded} {
		var ids []string
		for _, in := range Registry() {
			if in.SSRF == p {
				ids = append(ids, "`"+in.ID+"`")
			}
		}
		if len(ids) == 0 {
			continue // a posture with no integrations is not a row worth printing
		}
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", p, ssrfMeaning(p), strings.Join(ids, ", ")))
	}
	b.WriteString("\n")
	b.WriteString("`fixed-endpoint` rows (Resend, SMTP, HIBP) are recorded rather than omitted: they\n")
	b.WriteString("carry no user-supplied URL, so there is no dialer to guard — the next reader sees\n")
	b.WriteString("*why*, instead of assuming every outbound call is dial-guarded. An `unguarded` row\n")
	b.WriteString("would be a known gap with a named fix; there are none today.\n\n")

	b.WriteString("### Operator posture (issue #870)\n\n")
	b.WriteString("The `guarded-when-enabled` rows are only guarded when the operator opts in. The\n")
	b.WriteString("default (off) assumes a **trusted LAN** where webhooks and integrations\n")
	b.WriteString("legitimately target private hosts. On a deployment reachable from the internet, or\n")
	b.WriteString("one hosting accounts the operator does not vet, that set must be switched on —\n")
	b.WriteString("`WEBHOOK_BLOCK_PRIVATE_URLS`, `CALDAV_BLOCK_PRIVATE_URLS`,\n")
	b.WriteString("`IMMICH_BLOCK_PRIVATE_URLS`, `PAPERLESS_BLOCK_PRIVATE_URLS`,\n")
	b.WriteString("`SEAFILE_BLOCK_PRIVATE_URLS`, `WEBDAV_BLOCK_PRIVATE_URLS`,\n")
	b.WriteString("`MONICA_BLOCK_PRIVATE_URLS`, `OIDC_BLOCK_PRIVATE_URLS` all `true`. With them off,\n")
	b.WriteString("the app-layer guard is inactive and only a network egress policy stands between an\n")
	b.WriteString("authenticated user's integration URL and an internal or cloud-metadata address\n")
	b.WriteString("(`169.254.169.254`). Operator checklist: the \"SSRF hardening\" row in\n")
	b.WriteString("`docs/security/deployment-baseline.md`; `.env.example` groups the flags under\n")
	b.WriteString("\"SSRF hardening\".\n\n")
}

func writePerIntegration(b *strings.Builder) {
	b.WriteString("## Per-integration detail\n\n")
	b.WriteString("Each integration states its concrete required behavior for all seven failure modes\n")
	b.WriteString("from issue #464 point 2. The transient/permanent class for each mode is fixed by\n")
	b.WriteString("the table above; these cells say what \"handle it\" means operationally.\n\n")

	for _, in := range Registry() {
		b.WriteString(fmt.Sprintf("### %s\n\n", in.Name))
		b.WriteString(fmt.Sprintf("<a id=\"%s\"></a>\n\n", in.ID))
		b.WriteString(fmt.Sprintf("%s\n\n", in.What))

		b.WriteString(fmt.Sprintf("- **Criticality** — %s. %s\n", in.Criticality, in.CriticalityNote))
		b.WriteString(fmt.Sprintf("- **Direction** — %s.%s\n", in.Direction, noteSuffix(in.DirectionNote)))
		b.WriteString(fmt.Sprintf("- **Cadence** — %s. %s\n", in.Cadence, in.CadenceNote))
		b.WriteString(fmt.Sprintf("- **Data authority** — %s. %s\n", in.DataAuthority, in.DataAuthorityNote))
		b.WriteString(fmt.Sprintf("- **Failure impact** — %s. %s\n", in.FailureImpact, in.FailureImpactNote))
		b.WriteString(fmt.Sprintf("- **Timeout** — %s. %s\n", timeoutCell(in.Timeout), in.TimeoutNote))
		b.WriteString(fmt.Sprintf("- **Retry budget** — %s\n", in.RetryBudget))
		b.WriteString(fmt.Sprintf("- **SSRF** — %s. %s\n", in.SSRF, in.SSRFNote))
		b.WriteString(fmt.Sprintf("- **Source** — %s\n", codeList(in.SourceFiles)))
		b.WriteString(fmt.Sprintf("- **Failure behavior verified by** — %s\n\n", in.Verify))

		b.WriteString("| Failure mode | Class | Required behavior |\n")
		b.WriteString("|---|---|---|\n")
		d := Dispositions()
		for _, m := range FailureModes {
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", m, d[m].Persistence, in.Behavior[m]))
		}
		b.WriteString("\n")
	}
}

func writeAddingSection(b *strings.Builder) {
	b.WriteString("## Adding an integration\n\n")
	b.WriteString("1. Add a constructor to `backend/integrations/entries.go` and list it in `Registry()`.\n")
	b.WriteString("2. Fill every axis, the timeout, the retry budget, the SSRF posture, and all seven\n")
	b.WriteString("   `Behavior` cells. `TestRegistryInvariants` and `TestEveryFailureModeHasBehavior`\n")
	b.WriteString("   fail on a missing field.\n")
	b.WriteString("3. List the implementing `backend/services/*.go` files in `SourceFiles`.\n")
	b.WriteString("   `TestEveryOutboundClientIsClassified` fails if a `services` file opens an outbound\n")
	b.WriteString("   client and no row claims it (add it here, or — only for a client that reuses\n")
	b.WriteString("   another integration's transport — to `nonIntegrationClients` with a reason).\n")
	b.WriteString("4. Route the transport through `httputil.SafeDialContext` unless the destination is\n")
	b.WriteString("   a compiled-in vendor host. `TestSSRFClaimsMatchSource` enforces the claim.\n")
	b.WriteString("5. Regenerate this doc and commit the diff.\n\n")
	b.WriteString("Note the scope of the structural check: it covers `backend/services/`. A broader\n")
	b.WriteString("semgrep rule for *any* unguarded outbound client anywhere in the tree is issue\n")
	b.WriteString("#609; this matrix is the `services/`-scoped instance of that mechanism.\n\n")
}

func writeRelated(b *strings.Builder) {
	b.WriteString("## Related\n\n")
	b.WriteString("- **#465 (INT-02)** — makes each failure above actually happen and asserts the behavior.\n")
	b.WriteString("- **#466 (INT-03)** — retry safety for the outbound operations (idempotency, backoff, restart-survival).\n")
	b.WriteString("- **#467 (INT-04)** — the permanent-failure terminal state and staleness surfacing.\n")
	b.WriteString("- **#488 (DOC-03)** — operator-facing integration ownership; cites this document.\n")
	b.WriteString("- **#390 / #422 / #427 / #428** — the sync-health, delivery-health, last-known-good and alerting surfaces a failure must reach.\n")
	b.WriteString("- **#373 / #609** — webhook SSRF and the tree-wide unguarded-client rule.\n")
	b.WriteString("- `httputil.SafeDialContext` (`backend/httputil/safedial.go`) — the dial-time guard every guarded row relies on.\n")
}

// --- small helpers -----------------------------------------------------------

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func timeoutCell(d time.Duration) string {
	if d == 0 {
		return "_none wired_"
	}
	if d < time.Minute || d%time.Minute != 0 {
		return d.String()
	}
	// Render 60 * time.Second as "60s", not "1m0s".
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func ssrfMeaning(p SSRFPosture) string {
	switch p {
	case SSRFGuardedAlways:
		return "every connection through `SafeDialContext`, unconditionally"
	case SSRFGuardedWhenEnabled:
		return "guarded only when the operator opts in (`*_BLOCK_PRIVATE_URLS`)"
	case SSRFFixedEndpoint:
		return "no user-supplied URL — compiled-in vendor/operator host, no SSRF surface"
	case SSRFUnguarded:
		return "a user/operator URL reaches a transport that is **not** dial-guarded (known gap)"
	}
	return string(p)
}

func noteSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i+1]
	}
	return s
}

func codeList(files []string) string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = "`backend/" + f + "`"
	}
	return strings.Join(out, ", ")
}
