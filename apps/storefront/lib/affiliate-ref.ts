/**
 * The Storefront's memory of Affiliate Link clicks: which live code last brought
 * this visitor to which Event, so the checkout that follows — minutes or days
 * later — can name the link that drove it (ADR 0021).
 *
 * Affiliate Attribution is last-click within the Attribution Window: 7 days, per
 * Event, newest click winning. All three rules live here, in one first-party
 * cookie, and the Go API never sees any of them — it is handed a code at
 * begin-checkout and validates that code against the Event's live links. Moving
 * the window is therefore a change to this file and nothing else: no migration,
 * no API change.
 *
 * Framework-free on purpose, like checkout.ts: the I/O (setting the cookie when
 * an Event page is served with a ref, reading it in the begin-checkout route)
 * belongs to the middleware and the route handler, and the decisions belong
 * here where node:test can reach them.
 */

/** Name of the first-party cookie holding the remembered clicks. */
export const AFFILIATE_REF_COOKIE = "ticket_pos_affiliate_ref";

/**
 * The Attribution Window: how long a click is remembered. Generous enough to
 * credit "clicked on the bus, bought at home", and the accepted cost of that
 * generosity is that some of those buyers would have returned anyway — a price
 * worth paying for display-only stats (ADR 0021).
 */
export const ATTRIBUTION_WINDOW_DAYS = 7;

const DAY_MS = 24 * 60 * 60 * 1000;

/** The window in milliseconds, for entry expiry. */
export const ATTRIBUTION_WINDOW_MS = ATTRIBUTION_WINDOW_DAYS * DAY_MS;

/** The window in seconds, for the cookie's own Max-Age. */
export const ATTRIBUTION_WINDOW_SECONDS = ATTRIBUTION_WINDOW_MS / 1000;

/**
 * A code as an Affiliate Link issues it: unambiguous uppercase base32, eight
 * characters today. Matched loosely on length because the generator's width is
 * the backend's business, and strictly on alphabet because this value arrives
 * from a URL a stranger wrote — a ref is browser input like any other.
 *
 * Nothing here decides whether a code is LIVE. That is the API's verdict at
 * checkout, and asking it now would mean an API call on every Event page view
 * to answer a question whose answer can change before the buyer pays.
 */
const CODE_PATTERN = /^[0-9A-Z]{4,32}$/;

/**
 * How many Events' clicks are kept. A visitor browsing a lot of Events must not
 * grow a cookie that eventually gets rejected wholesale; the oldest clicks are
 * dropped first, which is the same last-click rule applied to the memory itself.
 */
const MAX_REMEMBERED_EVENTS = 20;

/** One Event's remembered click: the code, and when it stops counting. */
type RememberedClick = {
  code: string;
  /** Epoch milliseconds; the entry is gone from this moment on. */
  expiresAt: number;
};

/**
 * The per-Event key. Both slugs, because an Event is only unique within its
 * Organization, and scoping to the pair is what stops one Event's ref from
 * clobbering another's.
 */
function eventKey(orgSlug: string, eventSlug: string): string {
  return `${orgSlug.trim().toLowerCase()}/${eventSlug.trim().toLowerCase()}`;
}

/** Normalizes a ref from a URL into a code, or null when it could not be one. */
function normalizeCode(raw: string | null | undefined): string | null {
  if (typeof raw !== "string") return null;
  const code = raw.trim().toUpperCase();
  return CODE_PATTERN.test(code) ? code : null;
}

/**
 * Parses the cookie, dropping anything that is not a live remembered click:
 * malformed JSON, entries of the wrong shape, and entries whose window has
 * passed. A cookie is caller-controlled storage, so nothing in it is trusted on
 * the way out.
 */
function parseJar(raw: string | null | undefined, now: number): Record<string, RememberedClick> {
  if (typeof raw !== "string" || raw === "") return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};

  const jar: Record<string, RememberedClick> = {};
  for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (typeof value !== "object" || value === null) continue;
    const entry = value as Record<string, unknown>;
    const code = normalizeCode(typeof entry.code === "string" ? entry.code : null);
    const expiresAt = typeof entry.expiresAt === "number" ? entry.expiresAt : 0;
    if (!code || !Number.isFinite(expiresAt) || expiresAt <= now) continue;
    jar[key] = { code, expiresAt };
  }
  return jar;
}

/**
 * Remembers a click on an Affiliate Link and returns the cookie value to write,
 * or null when there was nothing worth remembering — an absent ref, or one that
 * could not be a code. A null answer means "leave the cookie alone": a junk ref
 * must not erase the real click that came before it.
 *
 * The newest click wins outright, including over itself: clicking the same link
 * again restarts that Event's window.
 */
export function rememberAffiliateClick(
  raw: string | null | undefined,
  orgSlug: string,
  eventSlug: string,
  ref: string | null | undefined,
  now: number,
): string | null {
  const code = normalizeCode(ref);
  if (!code) return null;

  const jar = parseJar(raw, now);
  jar[eventKey(orgSlug, eventSlug)] = { code, expiresAt: now + ATTRIBUTION_WINDOW_MS };

  // Newest first, then trimmed: what a visitor clicked most recently is what is
  // worth keeping when the jar is full.
  const kept = Object.entries(jar)
    .sort(([, a], [, b]) => b.expiresAt - a.expiresAt)
    .slice(0, MAX_REMEMBERED_EVENTS);
  return JSON.stringify(Object.fromEntries(kept));
}

/**
 * The code to carry into this Event's checkout, or null when no live click is
 * remembered. Never throws and never explains itself: an unattributed checkout
 * is the ordinary case.
 */
export function readAffiliateCode(
  raw: string | null | undefined,
  orgSlug: string,
  eventSlug: string,
  now: number,
): string | null {
  return parseJar(raw, now)[eventKey(orgSlug, eventSlug)]?.code ?? null;
}

/**
 * Cookie attributes, mirroring the checkout-context cookie's (checkout-context.ts):
 * httpOnly so page scripts cannot read or forge an attribution, Secure in
 * production, SameSite=Lax so arriving from Instagram or a QR code still carries
 * it, and path "/" because the click and the checkout happen on different
 * routes.
 *
 * Max-Age is the Attribution Window itself. The browser therefore drops the
 * whole cookie a week after the LAST click, while each Event's entry carries its
 * own expiry inside — so an old click is forgotten on schedule even when newer
 * ones keep the cookie alive.
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
