## Hard limits

- Never push, never merge, and never commit to `main`: merging to `main` deploys to production.
- Never run `terraform apply`, `terraform destroy` or anything else that changes real infrastructure; the operator applies Terraform by hand.
  `terraform fmt` and `terraform validate` are fine.
- The local database is a copy of production and holds real customer personal data.
  Never export it, copy rows out of it, paste its contents into issues or commits, or send it anywhere.
  Tests use their own testcontainers and fixtures, never that database.
- Never send real email, SMS or payment requests; use the fakes and test seams the codebase already has.

## Tooling quirks

- Read issues with `gh issue view <number> --json number,title,body,state,labels,comments`.
  The plain `gh issue view`, `gh pr view` and `gh pr edit` fail in this repo with a Projects (classic) GraphQL error.
- `make ci` is the verify command and mirrors CI.
  If `ci-preflight` stops it over root-owned `apps/*/.next` or `node_modules` entries, that is the environment, not your change: follow the fix it prints, never `sudo`.
- Regenerated artifacts (the OpenAPI spec and generated API client) must be regenerated with `make openapi` and committed, never edited by hand.
- Never edit `CHANGELOG.md` or any file marked as generated.

## Writing

- Never use the em dash; use a plain hyphen "-" instead.
  This applies to code, comments, docs, commit messages and issue comments.
- In long Markdown files you write or substantially edit, put each full sentence on its own line.
- Do not add a `Co-Authored-By` line or any agent attribution to commit messages.
- Domain terms come from `CONTEXT.md`; decisions you build on are in `docs/adr/`.
  When a ticket cites an ADR, read it before writing code.

## Quality

- If you see a lint error, a failing test or a flaky test, fix it even when your change did not cause it, in its own commit.
