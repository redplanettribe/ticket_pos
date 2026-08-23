/**
 * The Storefront's memory of an in-flight checkout: which event page it began
 * on and which language it began in, so the terminal pages can offer a way back
 * after the round trip through the Payment Provider, in the language the buyer
 * set off in.
 *
 * The provider's return redirect carries only our client transaction id and
 * the outcome — nothing about the Event, and nothing about the buyer — and the
 * confirm response is equally spare. Rather than widen the payment contract with
 * display concerns, the begin-checkout BFF route notes what the return leg will
 * need in a short-lived httpOnly cookie, and the handler and pages behind it
 * read it back. Losing the cookie (another browser, a cleared jar) degrades
 * gracefully: the pages still render, just without the event-specific link, and
 * the language falls back to the chain in redirect-locale.ts.
 *
 * IT IS NOT A SUBSTITUTE FOR A SESSION, AND IT IS NOT LEFTOVER FROM WHEN THERE
 * WAS NONE. Checkout requires a Customer Session since ADR 0054, and this cookie
 * survived that change because the two answer different questions: a session
 * says who is here NOW, and this says what was happening when the buyer left for
 * the Payment Provider. The gap between those two moments is minutes long, spans
 * another origin, and is the one place in the Storefront where a session can end
 * after the money has already moved (#387). Everything in here is what the
 * terminal pages have left when it does.
 *
 * This module owns the cookie; checkout-context-cookie.ts owns what is written
 * into it, framework-free so the parsing can be unit tested.
 */

import { cookies } from "next/headers";

import {
  checkoutContextLocale,
  parseCheckoutContext,
  serializeCheckoutContext,
  type CheckoutContext,
} from "./checkout-context-cookie";
import { type AppLocale } from "./locale";

export type { CheckoutContext };

/** Name of the httpOnly cookie holding the in-flight checkout's context. */
export const CHECKOUT_CONTEXT_COOKIE = "ticket_pos_checkout_context";

/**
 * A checkout is bounded by the provider's own windows (PayPhone's payment form
 * lives 10 minutes, confirm 5 more); half an hour comfortably outlives any
 * legitimate round trip without leaving stale context lying around for days.
 */
const CHECKOUT_CONTEXT_MAX_AGE_SECONDS = 30 * 60;

/**
 * Cookie attributes mirror the Customer Session cookie's (customer-session.ts):
 * httpOnly so page scripts cannot read it, Secure in production, SameSite=Lax —
 * which matters here: the provider's return redirect is a cross-site
 * navigation, and Lax is exactly what still sends the cookie on it.
 */
function checkoutContextCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    path: "/",
    maxAge: CHECKOUT_CONTEXT_MAX_AGE_SECONDS,
  };
}

/** Remembers the in-flight checkout, replacing any earlier one: one cart at a time. */
export async function rememberCheckoutContext(context: CheckoutContext): Promise<void> {
  const store = await cookies();
  store.set(
    CHECKOUT_CONTEXT_COOKIE,
    serializeCheckoutContext(context),
    checkoutContextCookieOptions(),
  );
}

/**
 * Reads the in-flight checkout's context, if any survives. Every field is
 * re-validated on the way out — a cookie is caller-controlled storage, and the
 * event path in particular becomes an href.
 */
export async function readCheckoutContext(): Promise<CheckoutContext | null> {
  const store = await cookies();
  return parseCheckoutContext(store.get(CHECKOUT_CONTEXT_COOKIE)?.value);
}

/**
 * The language the in-flight checkout began in, or null when the cookie is
 * gone, damaged, or was written before checkouts remembered one.
 *
 * Reads past everything else in the context on purpose (see
 * checkoutContextLocale): the provider's return leg runs after a real payment,
 * and losing a buyer's language there because some other field failed its guard
 * would be a poor trade for a decision that only picks a URL prefix.
 */
export async function readCheckoutLocale(): Promise<AppLocale | null> {
  const store = await cookies();
  return checkoutContextLocale(store.get(CHECKOUT_CONTEXT_COOKIE)?.value);
}
