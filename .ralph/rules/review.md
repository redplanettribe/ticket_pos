## Also check

- No em dash anywhere in the change: code, comments, docs or commit messages.
- No hand edits to `CHANGELOG.md` or generated files; regenerated OpenAPI artifacts are committed alongside the change that needs them.
- No real customer data from the local database in tests, fixtures, docs or commits.
- Nothing applies infrastructure: Terraform changes are code only, for the operator to apply by hand.
- Read issues with `gh issue view <number> --json number,title,body,state,labels,comments`; the plain `gh issue view` fails in this repo.
