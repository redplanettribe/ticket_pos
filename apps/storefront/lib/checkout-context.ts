/**
 * The Storefront's memory of an in-flight checkout: which event page it began
 * on, so the terminal pages can offer a way back after the round trip through
 * the Payment Provider.
 *
 * The provider's return redirect carries only our client transaction id and
 * the outcome — nothing about the Event — and the confirm response is equally
 * spare. Rather than widen the payment contract with display concerns, the
 * begin-checkout BFF route notes the event page in a short-lived httpOnly
 * cookie, and the success/failure pages read it back. Losing the cookie
 * (another browser, a cleared jar) degrades gracefully: the pages still render,
 * just without the event-specific link.
 */

import { cookies } from "next/headers";

import { safeEventPath } from "./checkout";
import { safePrefillEmail } from "./undo-window";

/** Name of the httpOnly cookie holding the in-flight checkout's context. */
export const CHECKOUT_CONTEXT_COOKIE = "ticket_pos_checkout_context";

/**
 * A checkout is bounded by the provider's own windows (PayPhone's payment form
 * lives 10 minutes, confirm 5 more); half an hour comfortably outlives any
 * legitimate round trip without leaving stale context lying around for days.
 */
const CHECKOUT_CONTEXT_MAX_AGE_SECONDS = 30 * 60;

export type CheckoutContext = {
  /**
   * Our id for the Payment attempt this context belongs to.
   *
   * Since #121 it is also the key the success page reads the Reversal Window
   * with. A guest who has just bought holds no Customer Session — checkout never
   * required one — so this id is the only thing that names their purchase, and
   * keeping it httpOnly means it stays with the browser that did the buying.
   */
  clientTransactionId: string;
  /** The event page the checkout began on, e.g. "/demo-venue/events/x". */
  eventPath: string;
  /** The Event's name, for copy on the terminal pages. */
  eventName: string;
  /**
   * The address the checkout was made under, so the success page can offer
   * sign-in already filled in (#121).
   *
   * It is a prefill and nothing else: the passcode still has to be proved, so
   * carrying it grants nobody anything. It matters because a purchase made under
   * one address and a session held under another belong to different Customers
   * (ADR 0011) — a buyer sent to sign in with their everyday email would land in
   * a Customer Area their new tickets are not in.
   *
   * Empty when the cookie predates this field or the buyer typed nothing usable;
   * the sign-in link then simply arrives blank.
   */
  customerEmail: string;
};

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
  store.set(CHECKOUT_CONTEXT_COOKIE, JSON.stringify(context), checkoutContextCookieOptions());
}

/**
 * Reads the in-flight checkout's context, if any survives. Every field is
 * re-validated on the way out — a cookie is caller-controlled storage, and the
 * event path in particular becomes an href.
 */
export async function readCheckoutContext(): Promise<CheckoutContext | null> {
  const store = await cookies();
  const raw = store.get(CHECKOUT_CONTEXT_COOKIE)?.value;
  if (!raw) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const candidate = parsed as Record<string, unknown>;
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
  if (!clientTransactionId || !eventPath) return null;
  return { clientTransactionId, eventPath, eventName, customerEmail };
}
