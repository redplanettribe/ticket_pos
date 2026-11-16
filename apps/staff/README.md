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

### Error screens and the reference code

A render crash shows a designed screen with a retry and a reference code instead of Next's default page (#772).
The presentational screen is `ErrorScreen` in `@ticket-pos/ui`, so the Storefront can reuse it; its words are the `errorScreen` namespace of the catalogs.

- Route level: `app/error.tsx`, for a page that crashed before its shell was drawn, inside the root layout and its providers.
  A crash in a page's content, under a `StaffPageShell` that did render, is caught inside the panels by `app/content-error-boundary.tsx`.
  An Event's pages have `app/events/[id]/error.tsx`, inside the Event's panel.
- Root level: `app/global-error.tsx`, for a crash in the root layout itself.
  It depends on no provider: the words come straight from the catalogs (the bundler keeps only that namespace), the language from the `NEXT_LOCALE` cookie and the browser.
- Retry asks the server for the failed segment again (`router.refresh()`) and renders it (`reset()`).
- A not-found or a redirect is control flow, never shown by these screens and never reported.

How the request id reaches the screen:

1. The middleware mints a UUID for every request it serves, never taking one from the browser.
   It forwards it on the request headers as `x-request-id`, echoes it in `X-Request-ID`, and passes it to its own session check.
2. `lib/api.ts` reads it from the request headers inside a render, route handler or server action, so every API call of that request carries it and the API logs each under it.
3. For a page or an RSC navigation, but not a BFF call under `/api/` or a prefetch, the middleware also sets the cookie `ticket_pos_request_id`, readable by script.
   An error screen is a client component and Next gives it only a digest for a server error, so the cookie is how it learns which request rendered the page.
4. A crash in a server render: `onRequestError` (`lib/request-error.ts`) writes one JSON line to stdout, which Cloud Logging indexes, with `request_id`, Next's `digest` and the route pattern; an event processor tags the server's Sentry event `request_id`; the screen reads the same id from the cookie the failed response set.
5. A crash only in the browser has no request of its own: the code is the id of the request that rendered the page the person is on (the document, or the last RSC navigation).
   The API's lines for that render are found by it, and the screen reports the error from the browser tagged `request_id` with it.

Reported exactly once: an error with a digest came from the server and was reported there, so the browser does not report it again; any other error is reported by the screen once (`lib/error-screen.ts`).
With no DSN the screens render the same and nothing is sent.

To walk the screens, add temporary throwing pages (not committed) for a page body, a server component inside `StaffPageShell`, a client component inside it, and the root layout, then run the build against the fake API and fake endpoint above.
Each crash shows its screen with a code equal to the document's `X-Request-ID`, the stand-in receives one event tagged with that `request_id`, the API stand-in saw that id, and the server log has the `request failed` line with it.
