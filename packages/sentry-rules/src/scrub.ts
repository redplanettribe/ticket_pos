/**
 * What a Next app may send to Sentry about a person: nothing (ADR 0079).
 *
 * The scrubber runs inside the app, before an event leaves it, and is the first
 * net; Sentry's server-side scrubbing is the second. It follows the backend's
 * rules so an event says the same amount wherever it was raised:
 *
 * - an email-shaped string becomes "[email]", including one URL-encoded into a
 *   path, which is where an email turns up in a route;
 * - an identity-number-shaped string becomes "[identity-number]";
 * - a URL loses its query string and fragment, where tokens and form values
 *   travel;
 * - cookies, headers, request bodies, the client's IP address and every user
 *   field but the opaque id are not attached at all, and neither are a stack
 *   frame's local variables.
 *
 * An identity number is matched by shape, not by check digit: a run of ten or
 * thirteen digits, standing alone, that starts with one of Ecuador's province
 * codes (01-24, or 30 for citizens registered abroad), the cédula and RUC
 * lengths of backend/internal/platform/taxid.go. Shape catches a mistyped
 * cédula as well as a valid one, and an Ecuadorian mobile number (09...) with
 * them. When in doubt, send less. An SRI clave de acceso (49 digits), a UUID
 * and an amount stand alone at other lengths and survive.
 */

import type { SentryBreadcrumb, SentryEvent, SentryException, SentrySpan, SentryStackFrame } from "./event.ts";

const EMAIL = /[A-Za-z0-9._%+-]+(?:@|%40)[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}/gi;

const IDENTITY_NUMBER = /(?<![0-9A-Za-z])(?:0[1-9]|1[0-9]|2[0-4]|30)(?:[0-9]{11}|[0-9]{8}|[0-9]{7}-[0-9])(?![0-9A-Za-z])/g;

// A query string or fragment following an absolute URL, or a path that starts
// a token. A "?" that ends a word in a sentence follows no URL and stays.
const URL_TAIL = /((?:\b[a-z][a-z0-9+.-]*:\/\/|(?<![\w.~%/-])\/)[^\s"'<>?#]*)[?#][^\s"'<>]*/gi;

// Breadcrumb and span data keys whose whole value is a URL, so everything from
// the first "?" or "#" goes, even on a relative URL that names no path.
const URL_KEYS = new Set(["url", "from", "to", "http.url", "url.full"]);

// Keys the SDK fills with a query string or fragment it has already split off.
const DROPPED_KEYS = new Set(["url.query", "url.fragment", "http.query", "http.fragment"]);

/** Redacts email and identity-number shapes and drops URL query strings in free text. */
export function scrubString(text: string): string {
  return text.replace(URL_TAIL, "$1").replace(EMAIL, "[email]").replace(IDENTITY_NUMBER, "[identity-number]");
}

/** A URL without its query string or fragment, then scrubbed like any text. */
export function scrubUrl(url: string): string {
  const cut = url.search(/[?#]/);
  return scrubString(cut === -1 ? url : url.slice(0, cut));
}

/**
 * A copy of the event holding nothing personal. The event handed in is not
 * changed. Generic so it serves `beforeSend` and `beforeSendTransaction` alike
 * and hands back the type it was given.
 */
export function scrubEvent<E extends SentryEvent>(event: E): E {
  const out: SentryEvent = { ...event };

  if (event.message !== undefined) out.message = scrubString(event.message);
  if (event.logentry) {
    out.logentry = { ...event.logentry };
    if (event.logentry.message !== undefined) out.logentry.message = scrubString(event.logentry.message);
    if (event.logentry.params) out.logentry.params = event.logentry.params.map(scrubValue);
  }
  if (event.transaction !== undefined) out.transaction = scrubUrl(event.transaction);
  if (event.exception) {
    out.exception = { ...event.exception };
    if (event.exception.values) out.exception.values = event.exception.values.map(scrubException);
  }
  if (event.breadcrumbs) out.breadcrumbs = event.breadcrumbs.map(scrubBreadcrumb);
  if (event.request) {
    // Only where the request went and how: no body, query, cookies, headers
    // or environment, which is where the client's address travels.
    const request: SentryEvent["request"] = {};
    if (event.request.url !== undefined) request.url = scrubUrl(event.request.url);
    if (event.request.method !== undefined) request.method = event.request.method;
    out.request = request;
  }
  if (event.user) {
    // A person is an opaque internal id, and nothing else.
    if (event.user.id === undefined) delete out.user;
    else out.user = { id: event.user.id };
  }
  if (event.tags) out.tags = scrubRecord(event.tags);
  if (event.extra) out.extra = scrubRecord(event.extra);
  if (event.contexts) out.contexts = scrubRecord(event.contexts);
  if (event.spans) out.spans = event.spans.map(scrubSpan);

  return out as E;
}

function scrubException(exception: SentryException): SentryException {
  const out: SentryException = { ...exception };
  if (exception.value !== undefined) out.value = scrubString(exception.value);
  if (exception.stacktrace?.frames) {
    out.stacktrace = { ...exception.stacktrace, frames: exception.stacktrace.frames.map(scrubFrame) };
  }
  return out;
}

function scrubFrame(frame: SentryStackFrame): SentryStackFrame {
  const { vars: _vars, ...out } = frame;
  if (frame.abs_path !== undefined) out.abs_path = scrubUrl(frame.abs_path);
  if (frame.filename !== undefined) out.filename = scrubUrl(frame.filename);
  return out;
}

function scrubBreadcrumb(breadcrumb: SentryBreadcrumb): SentryBreadcrumb {
  const out: SentryBreadcrumb = { ...breadcrumb };
  if (breadcrumb.message !== undefined) out.message = scrubString(breadcrumb.message);
  if (breadcrumb.data) out.data = scrubData(breadcrumb.data);
  return out;
}

function scrubSpan(span: SentrySpan): SentrySpan {
  const out: SentrySpan = { ...span };
  if (span.description !== undefined) out.description = scrubString(span.description);
  if (span.data) out.data = scrubData(span.data);
  return out;
}

function scrubData(data: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(data)) {
    if (DROPPED_KEYS.has(key)) continue;
    out[key] = URL_KEYS.has(key) && typeof value === "string" ? scrubUrl(value) : scrubValue(value);
  }
  return out;
}

function scrubRecord(record: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(record)) out[key] = scrubValue(value);
  return out;
}

function scrubValue(value: unknown): unknown {
  if (typeof value === "string") return scrubString(value);
  if (Array.isArray(value)) return value.map(scrubValue);
  if (value !== null && typeof value === "object") return scrubRecord(value as Record<string, unknown>);
  return value;
}
