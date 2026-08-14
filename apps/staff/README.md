# @ticket-pos/staff

Staff-facing Next.js app for Ticket POS.

Acts as a BFF for authenticated staff workflows: catalog management, in-person POS mode, and sale imports.
Auth is email OTP with server-side sessions (not yet implemented).

## Development

```bash
pnpm install
pnpm dev
```

Runs on port 3001 by default.

## Languages

English and Spanish, from `messages/en.json` and `messages/es.json` — read
`messages/README.md` before adding copy.

**next-intl is wired in without its routing half.** There is no `[locale]` URL
segment, no locale middleware, and no route path is a function of the language:
`/login` is one address that renders in whichever language its reader is owed.
The Storefront does the opposite, deliberately, because its pages are public and
shareable; a staff page is behind authentication and its language is a property
of the person reading it ([ADR
0041](../../docs/adr/0041-the-staff-locale-is-a-property-of-the-person-not-the-page.md)).

The locale is resolved once per request in `i18n/request.ts` and reaches a Server
Component through `getLocale()` / `getTranslations()` and a client one through
the provider in `app/layout.tsx`. Before sign-in the ladder is cookie, then
`Accept-Language`, then English — the shared one from `@ticket-pos/locale`, named
through `lib/staff-locale.ts`. The stored Staff Locale outranks both once there
is a session to read one from.

Locale logic that needs a test goes in `lib/`: the runner globs `lib/*.test.ts`
and sees nothing else.

**The whole application is translated** — the organizer-facing screens and the
Operator Dashboard alike (#281, landed one surface at a time from #286 to #292).

A literal string left in a component under `app/` is therefore a lint error, not
a surface awaiting its turn: `eslint.config.mjs` runs
`i18next/no-literal-string` at `error` over every `.tsx` in the app, and `pnpm turbo lint`
is what CI runs. What the allowlist covers, and why, is documented in
`messages/README.md`.
