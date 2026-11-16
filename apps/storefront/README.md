# @ticket-pos/storefront

Public, SEO-first ticket sales app for Ticket POS.

## Routes

- `/` - landing page
- `/{orgSlug}/events/{eventSlug}` - event detail (path-based tenancy)

## Development

```bash
npm install
npm run dev
```

Open [http://localhost:64300](http://localhost:64300) (via `make dev`), or the Next.js default `http://localhost:3000` if running `npm run dev` standalone.

## Error reporting (Sentry)

The app reports to its own Sentry project (ADR 0079), and does nothing until `SENTRY_DSN` is set.
Locally, in CI and in agent runs no DSN is set, so the SDK is never initialised on the server, in middleware or in the browser, no Sentry chunk is ever requested, and the tunnel route `/api/monitoring` answers 204 without forwarding.
Never set a real DSN on a local stack: it may be serving a copy of production data.

What it reads, all at runtime:

- `SENTRY_DSN`, which turns it on.
- `SENTRY_ENVIRONMENT`, `development` when unset; Terraform sets `production`.
- `SENTRY_RELEASE`, the commit SHA, from the image's `SENTRY_RELEASE` build argument; without it events carry no release.

The configuration lives in `lib/sentry.ts`, and reading it in `@ticket-pos/sentry-rules`, shared with the Staff app.
The server and middleware start in `instrumentation.ts`, and errors in server components, route handlers and middleware are reported through Next's `onRequestError`.

Source maps come from the build only when it is given `SENTRY_AUTH_TOKEN`, which only the deploy does (`sentryBuildOptions` in `@ticket-pos/sentry-rules`, #776).
Then `next build` uploads them to `ticket-pos-storefront`, failing soft, and the Dockerfile deletes every map left in the output and refuses an image that still holds one.
Without the token, as locally, in CI and in agent runs, the build plugin contacts nothing: no browser source maps, no upload, no release, no telemetry.
The release itself is created and each deploy marked by the deploy workflow (`scripts/sentry-release.sh`; terraform/README.md, "Sentry releases and source maps").

The browser is where the Storefront differs from the Staff app: a buyer's first load must not pay for monitoring.

- With a DSN, the root layout writes the configuration into `<meta name="ticket-pos-sentry">`, an inline handler first in the head (`lib/early-errors.ts`), and renders `components/sentry-loader.tsx`.
- The loader imports `lib/sentry-browser.ts`, and with it the SDK, when the browser is first idle after hydration; none of it is in the initial bundle.
- The inline handler keeps up to ten errors and rejections thrown before then; the SDK reports each once when it starts, and its own handlers report everything after.
- Page-load traces and Web Vitals are given up; errors and later navigations are captured, the navigations through Next's `onRouterTransitionStart` (`instrumentation-client.ts`, a forwarder that holds no Sentry) and named by page (`lib/navigation-route.ts`).
- Every event passes the shared before-send; in the browser, an exception with no frame from our own bundles is dropped, and on both sides so is an event from a known bot.

Browser envelopes go to `/api/monitoring`, which forwards only those addressed to this app's own project (`relayEnvelope` over `decideTunnel`, both from `@ticket-pos/sentry-rules`) and copies none of the browser's headers, so the client's address never reaches Sentry.
BFF calls to the API (`callBackend`, `fetchBackendRaw`) carry `sentry-trace` and `baggage` beside `X-Request-ID` (`lib/api.ts`).
The build plugin is configured to contact nothing: no source maps, no release, no telemetry, and no route manifest in the initial bundle (`next.config.ts`).

### Deep health, the uptime probe's target

`GET /api/health` (`app/api/health/route.ts`, `lib/deep-health.ts`) answers whether the Storefront can actually serve a buyer (#775).
It answers 200 `ok` only when it reaches the API and the API's `GET /api/v1/public/health` reached the database, and 503 `unavailable` otherwise, within four seconds (`DEEP_HEALTH_TIMEOUT_MS`); the uptime monitor's own timeout must be longer.
The body carries no data, nothing is cached, and the server's sampler never traces it.
A failing check is a log line, never a thrown error, so an outage raises the uptime alert and not one issue per probe.
The API is asked as every BFF call asks it, with the service credential and no session.

### Error screens

A render crash shows a designed screen instead of Next's default page, in the page's language, with a retry and a reference code (#774).

- Route level, `app/[locale]/error.tsx`: a page crashed.
  It renders inside the root layout, so next-intl's provider is there and the words are the page's locale; it stands alone with the logo, because each page draws its own shell.
- Root level, `app/global-error.tsx`: the root layout itself crashed, so it renders the whole document.
  It depends on nothing the crash may have taken down: its words come straight from the catalogs, its language from the address (`rootErrorScreenLocale`), and it has no provider, Toaster or Sentry loader.
- Both are `components/error-screen.tsx` around `ErrorScreen` from `@ticket-pos/ui`, the Staff app's screen, with the words of the `errorScreen` namespace.
- Retry asks the server for the failed segment again and renders it (`router.refresh()` then `reset()`), on the same address with its query string; the screen touches no cookie or storage, so a checkout in progress is where it was.
- A not-found is not an error: it is Next's 404, never these screens, and never reported.

The reference code is the request id, as in the Staff app, through the pieces both apps share in `@ticket-pos/sentry-rules`:

1. The middleware mints a UUID for every request it sees, never taking one from the browser, forwards it to the render as `x-request-id`, and echoes it in `X-Request-ID`.
   It does not run on the BFF routes under `/api/`, so those keep an id per API call.
2. `lib/api.ts` reads it inside a render or server action, so every API call of that page carries it and the API logs each under it.
3. For a page or an RSC navigation, not a prefetch, the middleware sets the cookie `ticket_pos_request_id`, readable by script, which the screens show.
   Like the Affiliate ref cookie, it rides beside the page and nothing renders from it.
4. A server crash: `onRequestError` writes one JSON line to stdout, which Cloud Logging indexes, with `request_id`, Next's `digest`, the route pattern and the render source, never the error's text or the path, with or without a DSN; the server's event is tagged `request_id`.
5. A crash only in the browser: the code is the id of the request that rendered the page, and the screen reports the error tagged with it.

Reported exactly once: an error with a digest came from the server and was reported there; any other is reported by the screen, once.
The screen may come before the SDK has loaded, so it loads the SDK's chunk itself to report (`lib/sentry-lazy.ts`, the same request the loader makes), with the configuration it read on its first render, before a root-level screen replaces the document that held it.
With no DSN it requests nothing, and the screens render the same.

### Walking it locally against a fake endpoint

Sentry itself is never contacted.
Run a stand-in that accepts and prints envelopes on `localhost:9999`, and one for the API, then:

```sh
pnpm build
SENTRY_DSN=http://publickey@localhost:9999/1 API_URL=http://localhost:<api stand-in> pnpm start
```

- A server component, a route handler and middleware that throw each produce one event, with `server_name` `ticket-pos-storefront` and no user or client address.
- In the browser, the chunks of `lib/sentry-browser.ts` are requested after the load event, and events are posted to `/api/monitoring` only.
- Use an ordinary browser user agent: a headless one is a bot, and its events are dropped.
- Without `SENTRY_DSN` the page has no meta, no inline handler and no loader, and nothing is sent.
- For the error screens, temporary pages that throw on the server, in the browser while hydrating and after, in the root layout on either side, and an RSC navigation into a crash: each shows its screen in the address's language, the code equals the document's `X-Request-ID`, the cookie, the `request failed` line, the event's `request_id` tag and the id the API stand-in saw, and each is one event.
  Without `SENTRY_DSN`, the same screens and codes, and nothing sent.
