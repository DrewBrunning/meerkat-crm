# Project Governance

Mycorrhizal CRM is a small, self-hosted open-source project maintained in the
maintainer's spare time. Governance is deliberately lightweight: a single
maintainer holds final authority, and this document exists so that
contributors, downstream users, and security researchers can see who that is,
what the roles are, and who holds access to the project's sensitive resources.

This file is kept current in the same pull request that changes any of the
facts below (a maintainer joining or leaving, an access grant or revocation).

## Project structure

There is one maintainer and no formal steering committee, working group, or
voting body. Decisions are made by the maintainer, in the open, on GitHub
issues and pull requests. Anyone may propose changes; the maintainer reviews
and merges them.

If the project grows enough to need shared ownership, this document is where
that change will be written down first.

## Roles and responsibilities

### Maintainer

**Drew Brunning — GitHub [@DrewBrunning](https://github.com/DrewBrunning)**

The maintainer is responsible for:

- Reviewing and merging pull requests, and being the final decision-maker on
  scope, design, and whether a change is accepted.
- Cutting releases (`.github/workflows/release.yml`) and the signing that goes
  with them (see [`docs/security/release-verification.md`](docs/security/release-verification.md)).
- Triaging security reports and coordinating disclosure per
  [`SECURITY.md`](SECURITY.md) (acknowledgement within 5 business days).
- Deciding dependency updates per
  [`docs/dependency-upgrade-policy.md`](docs/dependency-upgrade-policy.md),
  including security-fix response times.
- Administering the GitHub repository: branch protection / rulesets, required
  status checks, secrets, and collaborator access.
- Keeping the security documentation
  ([`docs/security/`](docs/security/)) and this governance file accurate.

### Contributor

Anyone who opens a pull request or issue. Contributors are responsible for:

- Following [`docs/development/contributing.md`](docs/development/contributing.md):
  one concern per PR, tests with the change, describing what and why.
- Signing off every commit under the Developer Certificate of Origin — see
  [Legal sign-off](#legal-sign-off) below.
- Responding to review feedback on their own pull requests.

Contributors have no merge rights and no access to the resources listed below.

## Members with access to sensitive resources

"Sensitive resources" are the things that could be used to publish a malicious
release, exfiltrate data, or take over project infrastructure. Today a single
person — the maintainer — holds all of them. There are no other collaborators
with write access, and no bots with write access beyond GitHub-native
Dependabot.

| Resource | Who has access | Notes |
|---|---|---|
| GitHub repository admin (`DrewBrunning/mycorrhizal-crm`) | Maintainer | Sole admin; no other users or teams with write access. |
| `main` branch-protection / ruleset bypass | Maintainer; the release GitHub App | The App (`RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY`) is on the bypass list only so a release tag push can trigger `docker-publish.yml`; it is used by one `workflow_dispatch`-only workflow and has no PR-triggered path. See [`docs/security/release-verification.md`](docs/security/release-verification.md). |
| Actions secrets: release App private key, Android `SIGNING_*` keystore secrets, `RESEND`/SMTP and other integration credentials used only by CI | Maintainer | Set and rotated by the maintainer. Image and SBOM signing is keyless (GitHub OIDC → Sigstore); there is no long-lived cosign private key. |
| Production deployment host (the maintainer's own server running a tagged release) | Maintainer | Not project infrastructure; operator-owned, per [`docs/security/threat-model.md`](docs/security/threat-model.md) → "The self-hosted boundary". |
| External project accounts: OpenSSF Best Practices / Baseline (project 14433), Codecov, OpenSSF Scorecard | Maintainer | Read-only badges; write access is the maintainer's GitHub identity. |

If anyone else is granted any of the above, this table and the roles section
are updated in the same pull request.

## Legal sign-off

Every commit merged into this repository must carry a `Signed-off-by:` line
asserting the contributor is legally entitled to submit the change under the
project's license. This is the [Developer Certificate of Origin](DCO) (DCO
1.1); add the line with `git commit -s`. The **DCO** status check
([`.github/workflows/dco.yml`](.github/workflows/dco.yml)) enforces it on every
pull request. Details and how to fix an unsigned branch are in
[`docs/development/contributing.md`](docs/development/contributing.md#sign-your-commits-dco).

## Decision-making and becoming a maintainer

- **Normal changes** land through a pull request the maintainer reviews and
  merges.
- **Disagreements** are resolved by discussion on the issue or PR; the
  maintainer has the final call.
- **Changes to this file** go through a pull request like anything else.
- **Becoming a maintainer** would follow sustained, high-quality contribution
  plus an explicit invitation from the current maintainer. There is no such
  process in flight; this line documents the intent should it arise.

## Contact

- Bugs, features, questions: [GitHub issues](https://github.com/DrewBrunning/mycorrhizal-crm/issues).
- Security reports: privately, per [`SECURITY.md`](SECURITY.md) (GitHub private
  security advisory).
