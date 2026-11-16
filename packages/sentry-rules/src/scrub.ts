/**
 * What a Next app may send to Sentry about a person: nothing (ADR 0079).
 *
 * The scrubber runs inside the app, before an event leaves it, and is the first
 * net; Sentry's server-side scrubbing is the second. ADR 0079 sets the same
 * rules for the backend's scrubber (#766), which had not landed when these
 * were written; the two must redact the same shapes:
 *
 * - an email-shaped string becomes "[email]", including one URL-encoded into a
 *   path, which is where an email turns up in a route;
 * - an identity-number-shaped string becomes "[identity-number]";
 * - a URL loses its query string and fragment, where tokens and form values
 *   travel; in free text, any word with a "?" inside it is cut there, since a
 *   relative URL ("api/orders?token=...") has no other mark of being one;
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

import type { SentryBreadcrumb, SentryEvent, SentryException, SentrySpan, SentryStackFrame, SentryStacktrace } from "./event.ts";

// Letters in any script: a buyer's address can be "josé.pérez@correo.ec".
const EMAIL = /[\p{L}\p{N}._%+-]+(?:@|%40)[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)*\.\p{L}{2,}/giu;

const IDENTITY_NUMBER = /(?<![0-9A-Za-z])(?:0[1-9]|1[0-9]|2[0-4]|30)(?:[0-9]{11}|[0-9]{8}|[0-9]{7}-[0-9])(?![0-9A-Za-z])/g;

// A fragment following an absolute URL, or a path that starts a token.
const URL_FRAGMENT = /((?:\b[a-z][a-z0-9+.-]*:\/\/|(?<![\w.~%/-])\/)[^\s"'<>?#]*)#[^\s"'<>]*/gi;

// A query string: a "?" inside a word, with more of the word after it. A "?"
// that ends a word in a sentence is followed by a space, and stays.
const QUERY_STRING = /([^\s"'<>?]+)\?[^\s"'<>]+/g;

// Breadcrumb and span data keys whose whole value is a URL, so everything from
// the first "?" or "#" goes, even on a relative URL that names no path.
const URL_KEYS = new Set(["url", "from", "to", "http.url", "url.full"]);

// Keys the SDK fills with a query string or fragment it has already split off.
const DROPPED_KEYS = new Set(["url.query", "url.fragment", "http.query", "http.fragment"]);

/** Redacts email and identity-number shapes and drops URL query strings in free text. */
export function scrubString(text: string): string {
  return text.replace(QUERY_STRING, "$1").replace(URL_FRAGMENT, "$1").replace(EMAIL, "[email]").replace(IDENTITY_NUMBER, "[identity-number]");
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
  if (event.threads) {
    out.threads = { ...event.threads, values: event.threads.values.map((thread) => ({ ...thread, stacktrace: scrubStacktrace(thread.stacktrace) })) };
  }
  if (event.fingerprint) out.fingerprint = event.fingerprint.map(scrubString);
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
  if (exception.type !== undefined) out.type = scrubString(exception.type);
  if (exception.value !== undefined) out.value = scrubString(exception.value);
  if (exception.mechanism?.data) out.mechanism = { ...exception.mechanism, data: scrubRecord(exception.mechanism.data) };
  if (exception.stacktrace) out.stacktrace = scrubStacktrace(exception.stacktrace);
  return out;
}

function scrubStacktrace<S extends SentryStacktrace | undefined>(stacktrace: S): S {
  if (!stacktrace?.frames) return stacktrace;
  return { ...stacktrace, frames: stacktrace.frames.map(scrubFrame) };
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
