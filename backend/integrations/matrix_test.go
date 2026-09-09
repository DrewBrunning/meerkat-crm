package integrations

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// committedMatrixRel is the generated artifact, relative to this package
// (backend/integrations -> repo docs/).
const committedMatrixRel = "../../docs/int-01-integration-classification-matrix.md"

// --- (1) the committed doc is exactly what the generator produces -------------

// TestMatrixReproducesCommittedDoc is the drift test (issue #464 "How to
// verify"): a Registry()/Dispositions() change that alters the matrix shows up
// as a reviewable diff, because this test fails until
// docs/int-01-integration-classification-matrix.md is regenerated.
func TestMatrixReproducesCommittedDoc(t *testing.T) {
	got := Render()
	committed, err := os.ReadFile(committedMatrixRel)
	if err != nil {
		t.Fatalf("reading committed matrix %s: %v", committedMatrixRel, err)
	}
	if string(committed) != got {
		t.Errorf("%s is stale: regenerate with `cd backend && go run ./cmd/genintegrationmatrix` "+
			"(or `make gen-integration-matrix`) and commit the diff", committedMatrixRel)
	}
}

// --- (2) registry shape ------------------------------------------------------

var (
	validCriticality  = map[Criticality]bool{CriticalityRequired: true, CriticalityOptional: true}
	validDirection    = map[Direction]bool{DirectionOutbound: true, DirectionInbound: true, DirectionBidirectional: true}
	validCadence      = map[Cadence]bool{CadenceInteractive: true, CadenceScheduled: true, CadenceEventDriven: true}
	validAuthority    = map[DataAuthority]bool{AuthorityRemote: true, AuthorityShared: true, AuthorityEnrichment: true, AuthorityNone: true}
	validImpact       = map[FailureImpact]bool{ImpactDegradedFeature: true, ImpactBlockedWorkflow: true, ImpactSilentStaleness: true}
	validSSRF         = map[SSRFPosture]bool{SSRFGuardedAlways: true, SSRFGuardedWhenEnabled: true, SSRFFixedEndpoint: true, SSRFUnguarded: true}
	kebab             = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	knownFailureModes = func() map[FailureMode]bool {
		m := make(map[FailureMode]bool, len(FailureModes))
		for _, fm := range FailureModes {
			m[fm] = true
		}
		return m
	}()
)

// TestRegistryInvariants pins the structural contract every row must satisfy so
// a half-filled entry cannot reach the generated doc.
func TestRegistryInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, in := range Registry() {
		if !kebab.MatchString(in.ID) {
			t.Errorf("integration ID %q is not kebab-case", in.ID)
		}
		if seen[in.ID] {
			t.Errorf("duplicate integration ID %q", in.ID)
		}
		seen[in.ID] = true

		if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.What) == "" {
			t.Errorf("%s: Name and What are required", in.ID)
		}
		if !validCriticality[in.Criticality] {
			t.Errorf("%s: unknown Criticality %q", in.ID, in.Criticality)
		}
		if !validDirection[in.Direction] {
			t.Errorf("%s: unknown Direction %q", in.ID, in.Direction)
		}
		if !validCadence[in.Cadence] {
			t.Errorf("%s: unknown Cadence %q", in.ID, in.Cadence)
		}
		if !validAuthority[in.DataAuthority] {
			t.Errorf("%s: unknown DataAuthority %q", in.ID, in.DataAuthority)
		}
		if !validImpact[in.FailureImpact] {
			t.Errorf("%s: unknown FailureImpact %q", in.ID, in.FailureImpact)
		}
		if !validSSRF[in.SSRF] {
			t.Errorf("%s: unknown SSRF posture %q", in.ID, in.SSRF)
		}
		if strings.TrimSpace(in.RetryBudget) == "" {
			t.Errorf("%s: RetryBudget is required (#466 needs it stated even when it is 'none')", in.ID)
		}
		if strings.TrimSpace(in.Verify) == "" {
			t.Errorf("%s: Verify (which ticket/test exercises the failure behavior) is required", in.ID)
		}
		if strings.TrimSpace(in.SSRFNote) == "" {
			t.Errorf("%s: SSRFNote is required — the posture alone is not enough", in.ID)
		}
		// Timeout: either a real deadline, or an explicit note explaining what
		// bounds the call instead. A zero timeout with no note is the bug.
		if in.Timeout == 0 && strings.TrimSpace(in.TimeoutNote) == "" {
			t.Errorf("%s: Timeout is 0 and TimeoutNote is empty — say what bounds the call", in.ID)
		}
		if in.Timeout < 0 {
			t.Errorf("%s: negative Timeout %s", in.ID, in.Timeout)
		}
		if len(in.SourceFiles) == 0 {
			t.Errorf("%s: SourceFiles is empty", in.ID)
		}
		for _, f := range in.SourceFiles {
			if !strings.HasPrefix(f, "services/") {
				t.Errorf("%s: SourceFiles entry %q must be under services/", in.ID, f)
			}
			if _, err := os.Stat(filepath.Join("..", f)); err != nil {
				t.Errorf("%s: SourceFiles entry %q does not exist: %v", in.ID, f, err)
			}
		}
		for m := range in.Behavior {
			if !knownFailureModes[m] {
				t.Errorf("%s: Behavior has an unknown failure mode key %q", in.ID, m)
			}
		}
	}
}

// TestEveryFailureModeHasBehavior is issue #464 point 2: every integration must
// have a stated behavior for all seven failure modes.
func TestEveryFailureModeHasBehavior(t *testing.T) {
	for _, in := range Registry() {
		if len(in.Behavior) != len(FailureModes) {
			t.Errorf("%s: %d Behavior entries, want %d", in.ID, len(in.Behavior), len(FailureModes))
		}
		for _, m := range FailureModes {
			if strings.TrimSpace(in.Behavior[m]) == "" {
				t.Errorf("%s: no behavior stated for failure mode %q", in.ID, m)
			}
		}
	}
}

// --- (3) the transient/permanent table is total and is the single source -----

// TestDispositionsCoverAllModes pins that Dispositions() — the table #465–#467
// test against — has an entry for every failure mode, each with a class and a
// rationale, and that the rate-limit row actually requires honoring Retry-After.
func TestDispositionsCoverAllModes(t *testing.T) {
	d := Dispositions()
	if len(d) != len(FailureModes) {
		t.Fatalf("Dispositions() has %d entries, want %d", len(d), len(FailureModes))
	}
	for _, m := range FailureModes {
		x, ok := d[m]
		if !ok {
			t.Errorf("Dispositions() missing %q", m)
			continue
		}
		if x.Persistence != Transient && x.Persistence != PermanentUntilHuman {
			t.Errorf("%q: unknown Persistence %q", m, x.Persistence)
		}
		if strings.TrimSpace(x.Rationale) == "" {
			t.Errorf("%q: empty Rationale", m)
		}
		if x.Persistence == PermanentUntilHuman && x.Retryable {
			t.Errorf("%q: permanent-until-human but marked Retryable", m)
		}
	}
	if !d[FailureRateLimited].HonorRetryAfter {
		t.Errorf("rate-limited must honor Retry-After (INT-03 #466)")
	}
	if d[FailureRateLimited].Persistence != Transient {
		t.Errorf("rate-limited must be transient")
	}
	if d[FailureAuthExpiry].Persistence != PermanentUntilHuman {
		t.Errorf("auth-expiry must be permanent-until-human")
	}
	if d[FailureRemoteResourceDeleted].Persistence != PermanentUntilHuman {
		t.Errorf("remote-resource-deleted must be permanent-until-human")
	}
}

// --- (4) the structural check ----------------------------------------------

// outboundClientSignal matches a line that opens an outbound network client.
// The set is deliberately concrete (not "http" anywhere) so it flags real
// clients, not incidental imports.
var outboundClientSignal = regexp.MustCompile(strings.Join([]string{
	`http\.Client\{`,
	`httputil\.SafeDialContext`,
	`resend\.NewClient`,
	`oidc\.NewProvider`,
	`smtp\.SendMail`,
	`smtp\.NewClient`,
	`webpush\.SendNotification`,
	`caldav\.NewClient`,
	`carddav\.NewClient`,
}, "|"))

// servicesWithOutboundClient scans backend/services for non-test files whose
// source trips outboundClientSignal, returning basenames.
func servicesWithOutboundClient(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "services")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	got := map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if outboundClientSignal.Match(body) {
			got[name] = true
		}
	}
	return got
}

// TestEveryOutboundClientIsClassified is the "a new integration added without a
// matrix row fails a structural check" requirement (issue #464). Every
// backend/services file that opens an outbound client must be claimed by a
// Registry() entry, or listed in nonIntegrationClients with a reason.
func TestEveryOutboundClientIsClassified(t *testing.T) {
	got := servicesWithOutboundClient(t)
	if len(got) < 10 {
		t.Fatalf("outbound-client scan matched only %d files — the signal regex is probably broken", len(got))
	}

	claimed := map[string]bool{}
	for _, in := range Registry() {
		for _, f := range in.SourceFiles {
			claimed[filepath.Base(f)] = true
		}
	}

	for name := range got {
		if claimed[name] {
			continue
		}
		if _, ok := nonIntegrationClients[name]; ok {
			continue
		}
		t.Errorf("backend/services/%s opens an outbound client but no integrations.Registry() "+
			"entry claims it — add a row to entries.go (or, if it reuses another integration's "+
			"transport, an entry to nonIntegrationClients with a reason)", name)
	}
}

// TestNonIntegrationAllowlistIsLive fails on a dead allowlist entry — a file in
// nonIntegrationClients that no longer trips the signal — so suppressions
// cannot pile up (same discipline as the citation-drift ignore file).
func TestNonIntegrationAllowlistIsLive(t *testing.T) {
	got := servicesWithOutboundClient(t)
	for name, reason := range nonIntegrationClients {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("nonIntegrationClients[%q] has no reason", name)
		}
		if !got[name] {
			t.Errorf("nonIntegrationClients[%q] no longer opens an outbound client — remove the dead entry", name)
		}
	}
}

// TestClaimedSourceFilesDoNotOverlap pins that two integrations do not both
// claim the same file unless that is genuinely one client serving both (the
// notification channels share notification_service.go by design; nothing else
// should).
func TestClaimedSourceFilesDoNotOverlap(t *testing.T) {
	owners := map[string][]string{}
	for _, in := range Registry() {
		for _, f := range in.SourceFiles {
			b := filepath.Base(f)
			owners[b] = append(owners[b], in.ID)
		}
	}
	allowedSharers := map[string]bool{
		"notification_service.go": true, // ntfy, gotify, webpush are one dispatcher
		"mailer.go":               true, // email-resend, email-smtp are one SendEmail
	}
	for f, ids := range owners {
		if len(ids) > 1 && !allowedSharers[f] {
			t.Errorf("%s is claimed by multiple integrations %v — split the file or the rows", f, ids)
		}
	}
}

// --- (5) SSRF claims trace to the source -----------------------------------

// TestSSRFClaimsMatchSource checks issue #464 point 5: a row claiming a guarded
// posture must actually route through httputil.SafeDialContext in at least one
// of its source files. Unguarded / fixed-endpoint rows make no such claim.
func TestSSRFClaimsMatchSource(t *testing.T) {
	for _, in := range Registry() {
		guarded := in.SSRF == SSRFGuardedAlways || in.SSRF == SSRFGuardedWhenEnabled
		if !guarded {
			continue
		}
		found := false
		for _, f := range in.SourceFiles {
			body, err := os.ReadFile(filepath.Join("..", f))
			if err != nil {
				t.Errorf("%s: reading %s: %v", in.ID, f, err)
				continue
			}
			// Either the file wires the guarded dialer directly, or it obtains
			// the shared SSRF-guarded delivery client via clientFor(cfg) (the
			// webhook_service.go accessor the notification channels reuse).
			src := string(body)
			if strings.Contains(src, "SafeDialContext") || strings.Contains(src, "clientFor(") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s claims SSRF posture %q but no source file wires SafeDialContext or uses clientFor()", in.ID, in.SSRF)
		}
	}
}

// TestByIDResolves is a small sanity check on the lookup helper.
func TestByIDResolves(t *testing.T) {
	if in, ok := ByID("oidc"); !ok || in.Name == "" {
		t.Errorf("ByID(oidc) = %+v, %v", in, ok)
	}
	if _, ok := ByID("does-not-exist"); ok {
		t.Errorf("ByID(does-not-exist) unexpectedly resolved")
	}
}

// TestRenderHelpers covers the render helpers on inputs the registry does not
// currently produce, so a future entry that does hit them is not the first
// exercise of the branch.
func TestRenderHelpers(t *testing.T) {
	if got := oneLine("no trailing period so the whole string comes back"); got != "no trailing period so the whole string comes back" {
		t.Errorf("oneLine(no-period) = %q", got)
	}
	if got := oneLine("first. second."); got != "first." {
		t.Errorf("oneLine(two sentences) = %q", got)
	}
	if got := ssrfMeaning(SSRFPosture("something-new")); got != "something-new" {
		t.Errorf("ssrfMeaning(unknown) = %q, want the raw value", got)
	}
	// Every known posture has a meaning, including ones the current registry
	// happens not to use (no `unguarded` integrations today).
	for _, p := range []SSRFPosture{SSRFGuardedAlways, SSRFGuardedWhenEnabled, SSRFFixedEndpoint, SSRFUnguarded} {
		if strings.TrimSpace(ssrfMeaning(p)) == "" {
			t.Errorf("ssrfMeaning(%q) is empty", p)
		}
	}
	if got := noteSuffix(""); got != "" {
		t.Errorf("noteSuffix(empty) = %q", got)
	}
}

// --- (6) INT-03 outbound-operation classification -------------------------

// TestOutboundOperationsClassified pins the structural contract of
// OutboundOperations() (issue #466 action 1): every row is complete, uses a
// known IdempotencyClass, points at a real Integration.ID, and — for anything
// that is not naturally idempotent — names a safeguard.
func TestOutboundOperationsClassified(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range OutboundOperations() {
		if !kebab.MatchString(op.ID) {
			t.Errorf("operation ID %q is not kebab-case", op.ID)
		}
		if seen[op.ID] {
			t.Errorf("duplicate operation ID %q", op.ID)
		}
		seen[op.ID] = true

		if _, ok := ByID(op.Integration); !ok {
			t.Errorf("%s: Integration %q is not in Registry()", op.ID, op.Integration)
		}
		if !validIdempotencyClass[op.Class] {
			t.Errorf("%s: unknown IdempotencyClass %q", op.ID, op.Class)
		}
		if strings.TrimSpace(op.Summary) == "" {
			t.Errorf("%s: Summary is required", op.ID)
		}
		if strings.TrimSpace(op.RetryPolicy) == "" {
			t.Errorf("%s: RetryPolicy is required (state it even when it is 'none')", op.ID)
		}
		if strings.TrimSpace(op.SourceRef) == "" {
			t.Errorf("%s: SourceRef (file:func) is required", op.ID)
		}
		if op.Class != NaturallyIdempotent && strings.TrimSpace(op.Safeguard) == "" {
			t.Errorf("%s: class %q needs a Safeguard — what makes a retry safe", op.ID, op.Class)
		}
	}
}

// TestOutboundOperationsCoverWriteIntegrations is the "a new outbound write
// added without a classification fails the check" guard. Every integration in
// knownOutboundWriteIntegrations must have at least one OutboundOperations()
// row; a dead entry (an ID no longer in Registry(), or one with no row) also
// fails, so the allowlist cannot rot. Same discipline as nonIntegrationClients.
func TestOutboundOperationsCoverWriteIntegrations(t *testing.T) {
	rows := map[string]int{}
	for _, op := range OutboundOperations() {
		rows[op.Integration]++
	}
	for id, reason := range knownOutboundWriteIntegrations {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("knownOutboundWriteIntegrations[%q] has no reason", id)
		}
		if _, ok := ByID(id); !ok {
			t.Errorf("knownOutboundWriteIntegrations names %q, not in Registry() — remove the dead entry", id)
		}
		if rows[id] == 0 {
			t.Errorf("integration %q performs remote writes but has no OutboundOperations() row", id)
		}
	}
	for _, op := range OutboundOperations() {
		if _, ok := knownOutboundWriteIntegrations[op.Integration]; !ok {
			t.Errorf("operation %q classifies integration %q, which is not in "+
				"knownOutboundWriteIntegrations — add it there with a reason", op.ID, op.Integration)
		}
	}
}

// TestDispositionForHTTPStatus pins the status→disposition mapping INT-03
// depends on: permanent statuses never come back Retryable, transient ones do,
// and 429/503 carry the Retry-After obligation.
func TestDispositionForHTTPStatus(t *testing.T) {
	permanent := []int{400, 401, 403, 404, 405, 409, 410, 422, 501, 418}
	for _, code := range permanent {
		if d := DispositionForHTTPStatus(code); d.Retryable || d.Persistence != PermanentUntilHuman {
			t.Errorf("status %d: got %+v, want permanent + not retryable", code, d)
		}
	}
	transient := []int{429, 500, 502, 503, 504}
	for _, code := range transient {
		if d := DispositionForHTTPStatus(code); !d.Retryable || d.Persistence != Transient {
			t.Errorf("status %d: got %+v, want transient + retryable", code, d)
		}
	}
	for _, code := range []int{429, 503} {
		if !DispositionForHTTPStatus(code).HonorRetryAfter {
			t.Errorf("status %d must honor Retry-After", code)
		}
	}
}

// TestRenderedDocHasRequiredSections guards the committed artifact's shape.
func TestRenderedDocHasRequiredSections(t *testing.T) {
	doc := Render()
	for _, want := range []string{
		"## Classification axes",
		"## Transient vs permanent — the per-error classification",
		"## Outbound operations — retry safety",
		"## The integrations",
		"## SSRF is a property of this whole surface",
		"## Per-integration detail",
		"## Adding an integration",
		"## Related",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered matrix is missing section %q", want)
		}
	}
	// Every integration has an anchored subsection.
	for _, in := range Registry() {
		if !strings.Contains(doc, "<a id=\""+in.ID+"\"></a>") {
			t.Errorf("rendered matrix has no anchor for %q", in.ID)
		}
	}
}

// TestSSRFSectionCarriesOperatorHardeningGuidance pins issue #870: the SSRF
// section must state that the opt-in guards default off for trusted-LAN
// self-hosting and must be switched on for an internet-exposed or multi-tenant
// deployment, and must point the operator at the deployment security baseline.
// A pen test read the "SSRF guards enforced in the transport dialer" claim and
// correctly reported that with the flags off the app-layer guard is inactive —
// this guidance is what keeps the running posture and the claim reconcilable.
func TestSSRFSectionCarriesOperatorHardeningGuidance(t *testing.T) {
	doc := Render()
	for _, want := range []string{
		"### Operator posture (issue #870)",
		"trusted LAN",
		"reachable from the internet",
		"WEBHOOK_BLOCK_PRIVATE_URLS",
		"OIDC_BLOCK_PRIVATE_URLS",
		"169.254.169.254",
		"docs/security/deployment-baseline.md",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("SSRF section is missing operator-hardening guidance %q", want)
		}
	}
}
