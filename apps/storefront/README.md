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

The browser is where the Storefront differs from the Staff app: a buyer's first load must not pay for monitoring.

- With a DSN, the root layout writes the configuration into `<meta name="ticket-pos-sentry">`, an inline handler first in the head (`lib/early-errors.ts`), and renders `components/sentry-loader.tsx`.
- The loader imports `lib/sentry-browser.ts`, and with it the SDK, when the browser is first idle after hydration; none of it is in the initial bundle.
- The inline handler keeps up to ten errors and rejections thrown before then; the SDK reports each once when it starts, and its own handlers report everything after.
- Page-load traces and Web Vitals are given up; errors and later navigations are captured, the navigations through Next's `onRouterTransitionStart` (`instrumentation-client.ts`, a forwarder that holds no Sentry) and named by page (`lib/navigation-route.ts`).
- Every event passes the shared before-send; in the browser, an exception with no frame from our own bundles is dropped, and on both sides so is an event from a known bot.

Browser envelopes go to `/api/monitoring`, which forwards only those addressed to this app's own project (`relayEnvelope` over `decideTunnel`, both from `@ticket-pos/sentry-rules`) and copies none of the browser's headers, so the client's address never reaches Sentry.
BFF calls to the API (`callBackend`, `fetchBackendRaw`) carry `sentry-trace` and `baggage` beside `X-Request-ID` (`lib/api.ts`).
The build plugin is configured to contact nothing: no source maps, no release, no telemetry, and no route manifest in the initial bundle (`next.config.ts`).

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
