/**
 * The browser half of lib/navigation-tracing.test.ts (#792): the Storefront's
 * lazy chunk (lib/sentry-browser.ts) started with the real browser SDK, in a
 * window just real enough for it, driven the way Next drives it, and what the
 * SDK did written to stdout as one line of JSON.
 *
 * It runs in a process of its own, started by the test with
 * `--conditions=browser`, the only way `@sentry/nextjs` resolves to its browser
 * build under Node. It is not a test file (the glob is lib/*.test.ts) and
 * nothing in the app imports it.
 *
 * Next, like a browser, is mimicked only where the SDK can tell: its popstate
 * listener, registered before the SDK loads, calls onRouterTransitionStart with
 * "traverse"; router.push calls it with "push" and pushes a history entry; and
 * `window.next.router` exists, as the SDK looks for it.
 *
 * Input, as argv[2]: the JSON list of steps, each `{ "push": "/es/x" }` or
 * `{ "back": true }`, with the `random` the step's Math.random returns, which
 * decides the step's sampling at the Storefront's one in ten.
 * Output: `{ navigations, headers }`, every navigation span the SDK started
 * (its trace, whether sampled, its navigation type) and, after each step, the
 * sentry-trace header a request made then would carry.
 */
import { register } from "node:module";

type Step = ({ push: string } | { back: true }) & { random: number };

// The SDK's browser build imports "next/router" as a bundler resolves it, with
// no extension, which Node's own resolution refuses; this gives it the ".js".
register(
  `data:text/javascript,${encodeURIComponent(`
export async function resolve(specifier, context, next) {
  try {
    return await next(specifier, context);
  } catch (error) {
    if (error?.code !== "ERR_MODULE_NOT_FOUND" || !/^next\\/[\\w/-]+$/.test(specifier)) throw error;
    return next(specifier + ".js", context);
  }
}`)}`,
);

const steps = JSON.parse(process.argv[2] ?? "[]") as Step[];

// The window: an EventTarget with a location, a history and a document.
const events = new EventTarget();
const origin = "https://storefront.test";
const entries: string[] = [`${origin}/es`];
let index = 0;
const location = {
  get href() {
    return entries[index];
  },
  get pathname() {
    return new URL(entries[index]).pathname;
  },
  get search() {
    return new URL(entries[index]).search;
  },
  get hash() {
    return "";
  },
  origin,
  protocol: "https:",
  host: "storefront.test",
  hostname: "storefront.test",
};
const history = {
  pushState(_state: unknown, _title: string, url: string) {
    entries.splice(index + 1, entries.length, new URL(url, location.href).href);
    index += 1;
  },
  replaceState(_state: unknown, _title: string, url: string) {
    entries[index] = new URL(url, location.href).href;
  },
  back() {
    index -= 1;
    events.dispatchEvent(new Event("popstate"));
  },
};
const document = Object.assign(new EventTarget(), {
  readyState: "complete",
  visibilityState: "visible",
  hidden: false,
  cookie: "",
  querySelector: () => null,
  getElementsByTagName: () => [],
  getElementById: () => null,
});
let random = 0.5;
Math.random = () => random;
Object.assign(globalThis, {
  window: globalThis,
  document,
  location,
  history,
  addEventListener: events.addEventListener.bind(events),
  removeEventListener: events.removeEventListener.bind(events),
  dispatchEvent: events.dispatchEvent.bind(events),
  // The tunnel; nothing leaves the process.
  fetch: async () => new Response(null, { status: 204 }),
});

const { routerTransitionStart } = await import("./sentry-navigation.ts");

// Next's router: there before the SDK, and the first to hear of a popstate.
const router = {
  push(href: string) {
    routerTransitionStart(href, "push");
    history.pushState(null, "", href);
  },
  replace() {},
  back: () => history.back(),
  forward() {},
};
Object.assign(globalThis, { next: { router } });
addEventListener("popstate", () => routerTransitionStart(location.href, "traverse"));

const Sentry = await import("@sentry/nextjs");
const { startBrowserSentry } = await import("./sentry-browser.ts");
startBrowserSentry(JSON.stringify({ dsn: "https://public@o0.ingest.sentry.io/1", environment: "test" }));

const navigations: Array<{ traceId: string; sampled: boolean; type: unknown }> = [];
Sentry.getClient()?.on("spanStart", (span) => {
  const { attributes = {} } = Sentry.spanToJSON(span) as unknown as { attributes?: Record<string, unknown> };
  if (attributes["sentry.op"] !== "navigation") return;
  navigations.push({
    traceId: span.spanContext().traceId,
    sampled: (span.spanContext().traceFlags & 1) === 1,
    type: attributes["navigation.type"],
  });
});

const headers: string[] = [];
for (const step of steps) {
  random = step.random;
  if ("push" in step) router.push(step.push);
  else router.back();
  headers.push(Sentry.getTraceData()["sentry-trace"] ?? "");
}

process.stdout.write(`${JSON.stringify({ navigations, headers })}\n`);
process.exit(0);
