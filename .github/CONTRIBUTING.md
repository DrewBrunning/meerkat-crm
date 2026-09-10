# Contributing to Mycorrhizal CRM

The full contributor guide lives at
**[`docs/development/contributing.md`](../docs/development/contributing.md)** —
setup, workflow, code style, and how to open a pull request.

Project roles, decision-making, and who holds access to sensitive resources are
in **[`GOVERNANCE.md`](../GOVERNANCE.md)**.

## Before your first pull request

- **One concern per PR**, with tests. Describe what changed and why.
- **Sign off every commit.** This project requires a
  [Developer Certificate of Origin](../DCO) sign-off on each commit — commit
  with `git commit -s`, which appends a `Signed-off-by:` line matching your
  author identity. The **DCO** status check enforces this on every pull
  request. To fix a branch whose commits are not signed off:

  ```sh
  git rebase --signoff origin/main
  git push --force-with-lease
  ```

- Report security issues privately instead — see [`SECURITY.md`](../SECURITY.md).
