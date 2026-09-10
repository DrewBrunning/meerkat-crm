---
title: Contributing
parent: Development
nav_order: 6
---

# Contributing

## Getting Started

1. Fork the repository and clone your fork locally.
2. Follow the setup steps in [Backend](backend.md) and [Frontend](frontend.md).
3. Create a feature branch from `main`.

## Development Workflow

1. Make your changes with tests.
2. Run `go test ./...` (backend) and the Playwright E2E suite (frontend)
3. Open a pull request against `main`.

## Code Style

**Backend:** Standard Go formatting (`gofmt`). Follow existing controller and service patterns with thin controllers, logic in services, always scope queries by `user_id`.

**Frontend:** TypeScript strict mode and MUI for all UI components. All user-facing strings through `i18next`. Custom hooks for data fetching, not inline `useEffect` + `fetch`.

## Pull Requests

- Keep PRs focused with one feature or fix per PR.
- AI tools may assist coding but you are responsible for the code quality. Do not open hands-off vibe-coded PRs. In those cases rather open a feature request instead.
- Describe what changed and why, not how.
- If the change is operator-visible — a database migration, a configuration
  variable, a behavior change, a deprecation, or anything needing action before
  or after upgrading — put an `## Upgrade notes` block in the PR description (and
  `## Breaking changes` if it applies). It is harvested into the release notes.
  If a migration/config change genuinely needs no note, write
  `no-changelog: <reason>` instead. See the
  [changelog policy](../changelog-policy.md).
- Sign off every commit — see below.

## Sign your commits (DCO)

Every commit merged here must carry a `Signed-off-by:` line certifying that you
wrote the change (or otherwise have the right to submit it) under this project's
licence. This is the [Developer Certificate of Origin](https://developercertificate.org/)
(DCO 1.1); the full text is in [`DCO`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/DCO)
at the repository root.

Add the line automatically when you commit:

```sh
git commit -s -m "your message"
```

It appends `Signed-off-by: Your Name <your@email>` using your configured
`user.name` / `user.email`, which must match the commit author.

If you already have unsigned commits on your branch, sign them all off and
force-push:

```sh
git rebase --signoff origin/main
git push --force-with-lease
```

The `DCO` GitHub Actions check runs on every pull request and fails if any
non-merge commit is missing a valid sign-off.

## Project governance

Roles, decision-making, and who holds access to sensitive project resources are
documented in [`GOVERNANCE.md`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/GOVERNANCE.md).

## Reporting Issues

Open an issue on [GitHub](https://github.com/DrewBrunning/mycorrhizal-crm/issues/new/choose). Try to include steps to reproduce for bugs. Use the feature request template for new ideas.
