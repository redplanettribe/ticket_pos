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

## Error reporting

The app reports to its own Sentry project (ADR 0079), and does nothing until `SENTRY_DSN` is set.
Locally, in CI and in agent runs no DSN is set, so the SDK is never initialised in the browser, on the server or in middleware, and the tunnel route `/api/monitoring` answers 204 without forwarding.
Never set a real DSN on a local stack: it may be serving a copy of production data.

What it reads, all at runtime:

- `SENTRY_DSN`, which turns it on.
- `SENTRY_ENVIRONMENT`, `development` when unset; Terraform sets `production`.
- `SENTRY_RELEASE`, the commit SHA, from the image's `SENTRY_RELEASE` build argument; without it events carry no release.

The configuration lives in `lib/sentry.ts`, the server and middleware start in `instrumentation.ts`, the browser in `instrumentation-client.ts`.
The browser gets the DSN from a `<meta>` the root layout writes, because the DSN is a value of the running service, not of the build.
Browser envelopes go to `/api/monitoring`, which forwards only those addressed to this app's own project (`lib/sentry-tunnel.ts`, over `decideTunnel` from `@ticket-pos/sentry-rules`) and copies none of the browser's headers, so the client's address never reaches Sentry.
Every event passes through the shared before-send from `@ticket-pos/sentry-rules`.
BFF calls to the API carry `sentry-trace` and `baggage` beside `X-Request-ID` (`lib/api.ts`).
The build plugin is configured to contact nothing: no source maps, no release, no telemetry (`next.config.ts`).

### Walking it locally against a fake endpoint

Sentry itself is never contacted.
Run a stand-in that accepts and prints envelopes on `localhost:9999` (any HTTP server that answers 200 and logs the body), then:

```sh
pnpm build
SENTRY_DSN=http://publickey@localhost:9999/1 pnpm start
```

- A server component, a route handler and middleware that throw each produce one envelope with an `event` item at `/api/1/envelope/`, with `server_name` `ticket-pos-staff` and no user or client address.
- A page that calls `notFound()` produces none.
- In the browser, `/login` carries `<meta name="ticket-pos-sentry">`, and an uncaught error is posted to `/api/monitoring`, which relays it to the stand-in with no `X-Forwarded-For`.
- An API stand-in at `API_URL` sees `sentry-trace`, `baggage` and `X-Request-ID` on every BFF call.
- Without `SENTRY_DSN`, none of this is sent, the meta is absent and the BFF calls carry `X-Request-ID` alone.
