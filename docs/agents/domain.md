# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

This repo is **single-context**: one glossary and one ADR directory, both at the root.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root — the canonical domain glossary.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-public-organization-endpoint.md
│   └── 0002-storefront-discovery-surfaces-and-discoverability.md
├── backend/                ← Go modular monolith
├── apps/                   ← Storefront and Staff Next.js apps
└── packages/               ← shared api-client and ui
```

The pnpm workspace boundary (`apps/*`, `packages/*`, `e2e`) is a **packaging** boundary, not a domain
boundary. The Go backend is a modular monolith and both frontends are surfaces onto the same domain, so
they all share the one glossary. If `catalog`, `sales`, and `identity` ever diverge into genuinely
separate languages, that is when to introduce a `CONTEXT-MAP.md` and per-context glossaries.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
