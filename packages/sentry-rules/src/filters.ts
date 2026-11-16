/**
 * Events dropped whole, before they are scrubbed or sent.
 *
 * A public site collects errors that are not ours to fix: a browser extension
 * throwing inside the page, a third-party script, a crawler's half-built
 * browser. They would spend the Storefront key's quota and bury real failures
 * (ADR 0079). Each filter is its own predicate, and createBeforeSend switches
 * each on separately: the Storefront uses both, the Staff app chooses.
 */

import type { SentryEvent, SentryStackFrame } from "./event.ts";
import { scrubEvent } from "./scrub.ts";

/** Where a Next app's browser bundles are served from. */
export const NEXT_BROWSER_BUNDLE_PATHS: readonly string[] = ["/_next/static/"];

/**
 * Whether any frame of any exception in the event comes from our own bundles:
 * a file under one of `ownBundlePaths`, served from the page's own origin.
 *
 * A bundle URL on another origin, such as another Next site's chunks embedded
 * in ours, is not ours. An event with no stack at all, such as a cross-origin
 * "Script error.", has no frame of ours.
 */
export function hasFrameFromOwnBundles(event: SentryEvent, ownBundlePaths: readonly string[] = NEXT_BROWSER_BUNDLE_PATHS): boolean {
  const pageOrigin = originOf(event.request?.url);
  for (const exception of event.exception?.values ?? []) {
    for (const frame of exception.stacktrace?.frames ?? []) {
      if (isOwnFrame(frame, ownBundlePaths, pageOrigin)) return true;
    }
  }
  return false;
}

function isOwnFrame(frame: SentryStackFrame, ownBundlePaths: readonly string[], pageOrigin: string | null): boolean {
  const location = frame.abs_path ?? frame.filename;
  if (location === undefined) return false;
  if (!ownBundlePaths.some((path) => location.includes(path))) return false;
  // A path with no scheme, or the app:// the SDK rewrites a path to, is the
  // page's own. An http(s) URL must be on the page's origin when that is
  // known. Any other scheme, such as chrome-extension://, is never ours.
  if (location.startsWith("/") || location.startsWith("app:///")) return true;
  const frameOrigin = originOf(location);
  if (frameOrigin === null) return false;
  return pageOrigin === null || frameOrigin === pageOrigin;
}

function originOf(url: string | undefined): string | null {
  if (url === undefined || !/^https?:\/\//i.test(url)) return null;
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

// Crawlers, link previewers, monitors and headless browsers. Matched on a
// "SomethingBot/1.0" product token, the names of the previewers that announce
// themselves without a version, or the "+https://..." contact URL a
// well-behaved crawler appends. A bare "bot" is not enough: phone makers such
// as Cubot put it in a person's user agent.
const BOT_USER_AGENT = new RegExp(
  [
    "bot\\/",
    "telegrambot",
    "slackbot",
    "discordbot",
    "spider",
    "crawl",
    "slurp",
    "facebookexternalhit",
    // The link previewer; WhatsApp's in-app browser carries a person's
    // ordinary user agent with "WhatsApp/x" after it.
    "^whatsapp\\/",
    "headlesschrome",
    "lighthouse",
    "phantomjs",
    "\\+https?:\\/\\/",
  ].join("|"),
  "i",
);

/** Whether the event came from a known bot, judged by the request's user agent. */
export function isFromBot(event: SentryEvent): boolean {
  const headers = event.request?.headers ?? {};
  const name = Object.keys(headers).find((key) => key.toLowerCase() === "user-agent");
  const userAgent = name === undefined ? undefined : headers[name];
  return userAgent !== undefined && BOT_USER_AGENT.test(userAgent);
}

export interface BeforeSendRules {
  /** Drop an event whose stack has no frame from our own bundles. */
  dropWithoutOwnFrame?: boolean;
  /** Drop an event from a known bot user agent. */
  dropBots?: boolean;
  /** Where the app's own bundles live, for dropWithoutOwnFrame; the Next browser bundles by default. */
  ownBundlePaths?: readonly string[];
}

/**
 * A `beforeSend` for one app: drops what the chosen filters reject, and scrubs
 * the rest. The filters run first because the bot check reads the user agent,
 * which the scrubber removes.
 *
 * The frame filter judges only an event that reports an exception: a captured
 * message carries no stack, and dropping it would silence what the app chose
 * to say. For the same reason this is for `beforeSend` only; hand
 * `beforeSendTransaction` plain `scrubEvent`.
 */
export function createBeforeSend(rules: BeforeSendRules = {}): <E extends SentryEvent>(event: E) => E | null {
  return (event) => {
    const reportsException = (event.exception?.values?.length ?? 0) > 0;
    if (rules.dropWithoutOwnFrame && reportsException && !hasFrameFromOwnBundles(event, rules.ownBundlePaths)) return null;
    if (rules.dropBots && isFromBot(event)) return null;
    return scrubEvent(event);
  };
}
