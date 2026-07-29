/**
 * The Storefront's memory of Affiliate Link clicks: which codes brought this
 * visitor to which Event, so the checkout that follows — minutes or days later
 * — can name the link that drove it (ADR 0022).
 *
 * Affiliate Attribution is last-click within the Attribution Window: 7 days, per
 * Event, newest click winning. All three rules live here, in one first-party
 * cookie, and the Go API never sees any of them — it is handed the codes at
 * begin-checkout and picks the first one that still names a live link.
 *
 * Why a short history per Event rather than one remembered code: liveness is
 * not knowable at click time (validating would mean an API call on every page
 * view, ADR 0022), so a click on a code that has since been deactivated would
 * otherwise erase the live click before it and cost that link its credit. The
 * list keeps the order — newest first — and the API applies the liveness rule
 * it alone can answer.
 *
 * Framework-free on purpose, like checkout.ts: the I/O (setting the cookie when
 * an Event page is served with a ref, reading it in the begin-checkout route)
 * belongs to the middleware and the route handler, and the decisions belong
 * here where node:test can reach them.
 */

import { normalizeAffiliateCode } from "./affiliate-code.ts";

/** Name of the first-party cookie holding the remembered clicks. */
export const AFFILIATE_REF_COOKIE = "ticket_pos_affiliate_ref";

/**
 * The Attribution Window: how long a click is remembered. Generous enough to
 * credit "clicked on the bus, bought at home", and the accepted cost of that
 * generosity is that some of those buyers would have returned anyway — a price
 * worth paying for display-only stats (ADR 0022).
 */
export const ATTRIBUTION_WINDOW_DAYS = 7;

const DAY_MS = 24 * 60 * 60 * 1000;

/** The window in milliseconds, for entry expiry. */
export const ATTRIBUTION_WINDOW_MS = ATTRIBUTION_WINDOW_DAYS * DAY_MS;

/** The window in seconds, for the cookie's own Max-Age. */
export const ATTRIBUTION_WINDOW_SECONDS = ATTRIBUTION_WINDOW_MS / 1000;

/**
 * How many Events' clicks are kept. A visitor browsing a lot of Events must not
 * grow a cookie that eventually gets rejected wholesale; the oldest clicks are
 * dropped first, which is the same last-click rule applied to the memory itself.
 */
const MAX_REMEMBERED_EVENTS = 20;

/**
 * How many codes are kept per Event. Three is enough for the case the history
 * exists for — a live click followed by a dead one, and one more — and small
 * enough that the API's resolution stays a handful of indexed lookups. The
 * oldest click on the Event falls off first.
 */
const MAX_CODES_PER_EVENT = 3;

/** One remembered click: the code, and when it stops counting. */
type RememberedClick = {
  code: string;
  /** Epoch milliseconds; the entry is gone from this moment on. */
  expiresAt: number;
};

/** One Event's remembered clicks, newest first. */
type RememberedClicks = RememberedClick[];

/**
 * The per-Event key. Both slugs, because an Event is only unique within its
 * Organization, and scoping to the pair is what stops one Event's ref from
 * clobbering another's.
 */
function eventKey(orgSlug: string, eventSlug: string): string {
  return `${orgSlug.trim().toLowerCase()}/${eventSlug.trim().toLowerCase()}`;
}

/**
 * Parses one Event's entry, dropping anything that is not a live remembered
 * click: entries of the wrong shape, codes that could not be codes, and clicks
 * whose own window has passed. Newest first, however the cookie was ordered.
 */
function parseClicks(value: unknown, now: number): RememberedClicks {
  if (!Array.isArray(value)) return [];
  const clicks: RememberedClicks = [];
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) continue;
    const { code: rawCode, expiresAt } = entry as Record<string, unknown>;
    const code = normalizeAffiliateCode(typeof rawCode === "string" ? rawCode : null);
    if (!code || typeof expiresAt !== "number" || !Number.isFinite(expiresAt) || expiresAt <= now) {
      continue;
    }
    // A code the cookie somehow lists twice is one memory, kept at its newest.
    if (clicks.some((click) => click.code === code)) continue;
    clicks.push({ code, expiresAt });
  }
  return clicks.sort((a, b) => b.expiresAt - a.expiresAt).slice(0, MAX_CODES_PER_EVENT);
}

/**
 * Parses the cookie, dropping anything that is not a live remembered click. A
 * cookie is caller-controlled storage, so nothing in it is trusted on the way
 * out.
 */
function parseJar(raw: string | null | undefined, now: number): Record<string, RememberedClicks> {
  if (typeof raw !== "string" || raw === "") return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};

  const jar: Record<string, RememberedClicks> = {};
  for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
    const clicks = parseClicks(value, now);
    if (clicks.length > 0) jar[key] = clicks;
  }
  return jar;
}

/**
 * Remembers a click on an Affiliate Link and returns the cookie value to write,
 * or null when there was nothing worth remembering — an absent ref, or one that
 * could not be a code. A null answer means "leave the cookie alone": a junk ref
 * must not erase the real click that came before it.
 *
 * The newest click goes to the front, including a repeat of one already
 * remembered: clicking the same link again moves it back to the front and
 * restarts its own window, rather than filling the history with itself.
 */
export function rememberAffiliateClick(
  raw: string | null | undefined,
  orgSlug: string,
  eventSlug: string,
  ref: string | null | undefined,
  now: number,
): string | null {
  const code = normalizeAffiliateCode(ref);
  if (!code) return null;

  const jar = parseJar(raw, now);
  const key = eventKey(orgSlug, eventSlug);
  const previous = (jar[key] ?? []).filter((click) => click.code !== code);
  jar[key] = [{ code, expiresAt: now + ATTRIBUTION_WINDOW_MS }, ...previous].slice(
    0,
    MAX_CODES_PER_EVENT,
  );

  // Newest first, then trimmed: what a visitor clicked most recently is what is
  // worth keeping when the jar is full. An Event's freshness is its newest
  // click, which is the entry at the front of its own list.
  const kept = Object.entries(jar)
    .sort(([, a], [, b]) => b[0].expiresAt - a[0].expiresAt)
    .slice(0, MAX_REMEMBERED_EVENTS);
  return JSON.stringify(Object.fromEntries(kept));
}

/**
 * The codes to carry into this Event's checkout, newest click first, or an
 * empty list when nothing live is remembered. Never throws and never explains
 * itself: an unattributed checkout is the ordinary case.
 *
 * The order is the whole message. The API credits the first code that still
 * names a live Affiliate Link, so "newest first" is last-click attribution and
 * a dead code simply falls through to the click before it.
 */
export function readAffiliateCodes(
  raw: string | null | undefined,
  orgSlug: string,
  eventSlug: string,
  now: number,
): string[] {
  return (parseJar(raw, now)[eventKey(orgSlug, eventSlug)] ?? []).map((click) => click.code);
}

/**
 * Cookie attributes, mirroring the checkout-context cookie's (checkout-context.ts):
 * httpOnly so page scripts cannot read or forge an attribution, Secure in
 * production, SameSite=Lax so arriving from Instagram or a QR code still carries
 * it, and path "/" because the click and the checkout happen on different
 * routes.
 *
 * Max-Age is the Attribution Window itself. The browser therefore drops the
 * whole cookie a week after the LAST click, while each remembered click carries
 * its own expiry inside — so an old click is forgotten on schedule even when
 * newer ones keep the cookie alive.
 */
export function affiliateRefCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    path: "/",
    maxAge: ATTRIBUTION_WINDOW_SECONDS,
  };
}
