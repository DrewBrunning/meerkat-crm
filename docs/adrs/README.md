# Architecture Decision Records

Decisions that materially shaped the codebase and would be costly to reverse, recorded as they were
made. The original planning documents they superseded lived in `docs/fork-plan/`; the working
ticket backlog moved to GitHub Issues.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-neutral-hub-and-spoke-contact-model.md) | Neutral hub-and-spoke contact model | accepted |
| [0002](0002-correspondence-table-locked-oracle.md) | Correspondence table as the locked mapping oracle | accepted |
| [0003](0003-golden-fixtures-external-test-oracle.md) | Golden fixtures as the external test oracle | accepted |
| [0004](0004-soft-vs-hard-delete-semantics.md) | Soft vs hard delete semantics | accepted |
| [0005](0005-operational-event-model.md) | Operational-event model, separate from the audit trail | accepted |
| [0006](0006-revision-token-schema.md) | Monotonic per-row revision tokens | accepted |
| [0007](0007-source-import-mapping.md) | Source imports: Meerkat direct-DB + Monica snapshot over one shared mapping framework | accepted |
| [0008](0008-conditional-write-enforcement.md) | REST conditional-write enforcement (If-Match / optimistic concurrency) | accepted |
| [0009](0009-rest-conflict-policy.md) | REST write-conflict policy (reject-and-return, per entity shape) | accepted |
| [0010](0010-idempotency-keys.md) | One idempotency mechanism — a client-supplied `Idempotency-Key` | accepted |
| [0011](0011-scheduled-job-catchup.md) | Scheduled-job catch-up semantics — fire missed occurrences once, de-duplicated | accepted |
| [0012](0012-canonical-database-invariants.md) | Canonical database invariants | accepted |
| [0013](0013-outbound-retry-safety-and-terminal-failures.md) | Outbound retry safety and permanent-failure terminal state | accepted |
| [0014](0014-local-app-lock-and-biometric-resume.md) | Local app lock and biometric resume (Android) | accepted |
