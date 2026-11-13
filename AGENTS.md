# Ticket POS

A point-of-sale system for organizations to create events, define ticket types, and sell tickets
online or in person, with programmatic access for Integration Partners.

Start with [CONTEXT.md](./CONTEXT.md) for the domain glossary and
[docs/technical-design.md](./docs/technical-design.md) for architecture.

## Agent skills

### Issue tracker

Issues live as GitHub issues in `redplanettribe/ticket_pos`, managed with the `gh` CLI.
See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, each label string equal to its name.
See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root.
See `docs/agents/domain.md`.

<!-- ralph:begin. Written by `ralph setup`, which refreshes everything up to ralph:end. -->
## ralph

This project runs [ralph](https://github.com/pedrodcsjostrom/ralph), which implements a spec's tickets unattended, one at a time, with Claude Code.
Run it through the wrapper at the root, `./ralph`; `./ralph help` documents every command, the configuration in `.ralph/config` and the project rules in `.ralph/rules/`.
Issues, specs and tickets live on GitHub, as `docs/agents/issue-tracker.md` describes.
<!-- ralph:end -->
