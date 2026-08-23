/**
 * The checkout context's payload: what a beginning checkout writes down, and
 * what may be trusted when it comes back.
 *
 * Split out of checkout-context.ts, which owns the cookie itself, because that
 * module reaches for next/headers and this half must stay framework-free. The
 * return leg from the Payment Provider is the most consequential path in the
 * Storefront — the buyer has already paid by the time it runs — so every way a
 * cookie can arrive damaged, stale, or shaped by an older release is worth a
 * unit test rather than a manual round trip through a provider.
 *
 * The ".ts" in the local imports is written out because these tests run this
 * module directly under `node --experimental-strip-types`, which resolves
 * specifiers exactly. Next resolves it identically (see lib/locale.ts).
 */

import { safeEventPath } from "./checkout.ts";
import { decodeSelection, encodeSelection } from "./selection-url.ts";
import { isAppLocale, localePrefixOf, type AppLocale } from "./locale.ts";
import { safePrefillEmail } from "./signin-prefill.ts";

export type CheckoutContext = {
  /**
   * Our id for the Payment attempt this context belongs to.
   *
   * Since #121 it is also the key the success page reads the Reversal Window
   * with, and it is the only thing that names the purchase to a browser holding
   * no session — which since ADR 0054 means a buyer whose session did not
   * survive the trip to the Payment Provider rather than a guest (#387). Keeping
   * it httpOnly means it stays with the browser that did the buying.
   */
  clientTransactionId: string;
  /** The event page the checkout began on, e.g. "/demo-venue/events/x". */
  eventPath: string;
  /** The Event's name, for copy on the terminal pages. */
  eventName: string;
  /**
   * The address the checkout was made under, so the terminal pages can offer
   * sign-in already filled in (#121).
   *
   * ITS RATIONALE CHANGED WITH ADR 0054 AND ITS JOB DID NOT (#387). It used to
   * be here because the buyer never had a session — checkout was guest-facing,
   * and this was the only thing that named them. It is here now because THE
   * BUYER'S SESSION MAY NOT HAVE SURVIVED THE ROUND TRIP: they left this origin
   * for the Payment Provider, possibly for minutes, and a cleared jar, a provider
   * webview that drops cookies, a session revoked elsewhere or a return in a
   * different browser each land somebody who has already paid on a page with no
   * session. Neither reading makes it optional.
   *
   * SINCE #387 IT IS THE API'S ANSWER AND NOT THE BROWSER'S. The session-gated
   * begin-checkout reports the address it addressed the Ticket Sale to, and the
   * BFF writes that down; nothing a page could say about who was buying reaches
   * here, because a browser-supplied address is the field ADR 0054 deleted
   * wearing a different name.
   *
   * It is a prefill and nothing else: the passcode still has to be proved, so
   * carrying it grants nobody anything. It matters because a purchase made under
   * one address and a session held under another belong to different Customers
   * (ADR 0011) — a buyer sent to sign in with their everyday email would land in
   * a Customer Area their new tickets are not in.
   *
   * Empty when the cookie predates this field, or was written by the one release
   * that wrote none, or holds nothing usable; the sign-in link then simply
   * arrives blank, which is a buyer typing their own address rather than a buyer
   * shown a wrong one.
   */
  customerEmail: string;
  /**
   * The basket this checkout was for, encoded exactly as it travels in an
   * address (ADR 0054, lib/selection-url.ts) — so a Payment that was declined
   * hands the buyer back the selection they were about to pay for instead of an
   * empty Event page.
   *
   * It is stored rather than rebuilt because this is the last moment anything
   * knows it: the buyer leaves this origin for the Payment Provider, and the
   * page state that held the quantities is gone by the time they come back.
   *
   * A SUGGESTION AND NOT A COMMAND, like every other spelling of a selection.
   * It names Ticket Types and quantities, cannot spell a price or a capacity,
   * and is re-judged against the Event by the page that receives it — a Ticket
   * Type that sold out while the buyer was at the provider comes back reported
   * rather than restored.
   *
   * Empty when the cookie predates this field or nothing survived its guards,
   * in which case the retry link is exactly the bare Event page it always was.
   */
  selection: string;
  /**
   * The language the buyer was reading the Event page in when they set off.
   *
   * The Payment Provider's return URL is a fixed constant the API hands over at
   * Payment time, so it names no language and the handler behind it has to
   * choose one. Without this it could only guess from the switcher's cookie and
   * Accept-Language — which sends a visitor reading /es on an English-language
   * browser back into English, having paid.
   *
   * Null means this cookie has nothing to say: it predates the field, or holds a
   * language this Storefront no longer serves. NULL IS NOT ENGLISH. Defaulting
   * it here would turn every cookie written by an older release into a confident
   * assertion of English and strand exactly the buyer this field exists for; the
   * return leg falls back to the ordinary chain instead, which is precisely
   * where it stood before the field existed.
   *
   * It travels no further than this browser. Nothing about language crosses the
   * payment contract — the Go API stays Locale-unaware.
   */
  locale: AppLocale | null;
};

/** The context as it is stored, one cart's worth. */
export function serializeCheckoutContext(context: CheckoutContext): string {
  return JSON.stringify(context);
}

/**
 * The stored context, or null when nothing usable survives.
 *
 * Every field is re-validated on the way out — a cookie is caller-controlled
 * storage however httpOnly it is, and the event path in particular becomes an
 * href. A context missing the two fields that identify the purchase is no
 * context at all, because everything the terminal pages do with it is keyed by
 * those.
 */
export function parseCheckoutContext(raw: string | null | undefined): CheckoutContext | null {
  const candidate = readCookieObject(raw);
  if (!candidate) return null;

  const clientTransactionId =
    typeof candidate.clientTransactionId === "string" ? candidate.clientTransactionId : "";
  const eventPath = safeEventPath(
    typeof candidate.eventPath === "string" ? candidate.eventPath : null,
  );
  const eventName = typeof candidate.eventName === "string" ? candidate.eventName : "";
  // The address is guarded on the way out like everything else here: it becomes
  // a query parameter on a sign-in link, and a cookie is caller-controlled
  // storage however httpOnly it is.
  const customerEmail = safePrefillEmail(
    typeof candidate.customerEmail === "string" ? candidate.customerEmail : null,
  );
  // Round-tripped through the selection format's own decoder rather than
  // pattern-matched here: a cookie is caller-controlled storage however
  // httpOnly it is, and anything the decoder refuses — malformed, duplicated,
  // oversized, hostile — comes back as the empty selection and is written out
  // as the empty string. One guard, in the module that owns the format.
  const selection = encodeSelection(
    decodeSelection(typeof candidate.selection === "string" ? candidate.selection : null),
  );
  if (!clientTransactionId || !eventPath) return null;
  const locale = readLocale(candidate);
  return { clientTransactionId, eventPath, eventName, customerEmail, selection, locale };
}

/**
 * Only the language out of a stored context, ignoring the state of the rest.
 *
 * Deliberately not `parseCheckoutContext(raw)?.locale`. That reading is all or
 * nothing — right for the terminal pages, which have nothing to say about a
 * purchase they cannot name — but the wrong bargain here: a context whose event
 * path failed its guard would cost a buyer their language on top of it, for a
 * decision that only picks which prefix to redirect into.
 */
export function checkoutContextLocale(raw: string | null | undefined): AppLocale | null {
  const candidate = readCookieObject(raw);
  return candidate ? readLocale(candidate) : null;
}

/**
 * The language the page that began a checkout was being read in, taken from the
 * Referer of the browser's call to the begin-checkout route.
 *
 * The fallback, not the first answer: the page states its own locale in the
 * request body, and that is what the route prefers. This reads a header the
 * browser may simply not send.
 *
 * That route's own address carries no locale — it is one of the unprefixed BFF
 * paths — but the page calling it is always a prefixed Event page, and a
 * same-origin fetch carries that page's full URL under the browsers' default
 * referrer policy. Null when the header is missing, unparseable, or names no
 * language this Storefront serves, which a privacy extension or a stricter
 * policy can each produce; the buyer then simply keeps the guess they had
 * before.
 *
 * The Referer's origin is not checked, because it decides nothing but which of
 * this Storefront's own prefixes a redirect lands on: the only strings that
 * survive are "en" and "es".
 */
export function checkoutLocaleFromReferer(referer: string | null | undefined): AppLocale | null {
  if (!referer) return null;
  let url: URL;
  try {
    url = new URL(referer);
  } catch {
    return null;
  }
  return localePrefixOf(url.pathname);
}

/** The cookie's JSON, or null for anything that is not an object — it never throws. */
function readCookieObject(raw: string | null | undefined): Record<string, unknown> | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  return parsed as Record<string, unknown>;
}

function readLocale(candidate: Record<string, unknown>): AppLocale | null {
  return isAppLocale(candidate.locale) ? candidate.locale : null;
}
