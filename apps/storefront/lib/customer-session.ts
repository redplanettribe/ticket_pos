/**
 * The Storefront's half of Customer identity: the cookie that holds a Customer
 * Session token, and the server-side reads that turn it into a session or a
 * Customer Area.
 *
 * Everything here runs on the server. The token never reaches page scripts: it
 * lives in an httpOnly cookie that only route handlers and server components can
 * read, so a script injection on a Storefront page has nothing to lift
 * (ADR 0010, PRD user story 36). The browser's only way to use it is to call one
 * of this app's own /api/customer/... route handlers, which is also the only
 * shape ADR 0008 allows — no browser code may address the Go API.
 *
 * The cookie is deliberately named differently from the Staff app's
 * `ticket_pos_session`. The two apps are separate origins and could not share a
 * cookie in any case, but a distinct name means that reading either app's code
 * makes the independence obvious: signing out here destroys a Customer Session
 * and can touch nothing of a Staff Session.
 */

import { cookies } from "next/headers";

import { APIError, callBackend } from "./api";

/** Name of the httpOnly cookie holding the Customer Session token. */
export const CUSTOMER_SESSION_COOKIE = "ticket_pos_customer_session";

/**
 * The Customer Session cookie's lifetime, set to the same 180 days as the API's
 * server-side session row.
 *
 * The two halves are not kept in step. The API's expiry slides on every
 * authenticated read; the cookie does not — it is written once, at sign-in or at
 * Confirmation Link redemption, and no read path re-issues it. So the cookie
 * expires 180 days after sign-in however active the Customer has been, and a
 * session still alive on the server can lose its cookie underneath it. Matching
 * the two numbers only means that a Customer who never returns loses both at
 * once; it does not make the cookie track the server's sliding window.
 */
const CUSTOMER_SESSION_MAX_AGE_SECONDS = 180 * 24 * 60 * 60;

/**
 * cookieOptions mirrors the Staff app's attributes exactly (apps/staff/lib/session.ts):
 * httpOnly so page scripts cannot read it, Secure in production, SameSite=Lax so
 * a normal link into the Customer Area still carries it while cross-site form
 * posts do not, and path "/" so every Storefront route sees it.
 */
export function customerSessionCookieOptions() {
  const secure = process.env.NODE_ENV === "production";
  return {
    httpOnly: true,
    secure,
    sameSite: "lax" as const,
    path: "/",
    maxAge: CUSTOMER_SESSION_MAX_AGE_SECONDS,
  };
}

/**
 * A Confirmation Link session lasts about a day, not six months, so its cookie
 * is capped to match. The narrowness is the point: a confirmation email gets
 * forwarded, and the session it mints must close within a day even though the
 * link that minted it stays valid until after the Event.
 */
const CONFIRMATION_LINK_SESSION_MAX_AGE_SECONDS = 24 * 60 * 60;

export function confirmationLinkSessionCookieOptions() {
  return { ...customerSessionCookieOptions(), maxAge: CONFIRMATION_LINK_SESSION_MAX_AGE_SECONDS };
}

/** Attributes that erase the cookie, used on sign-out and on a dead session. */
export function clearedCustomerSessionCookieOptions() {
  return { ...customerSessionCookieOptions(), maxAge: 0 };
}

/** Reads the Customer Session token from the request's cookies, if any. */
export async function customerSessionToken(): Promise<string | undefined> {
  const store = await cookies();
  return store.get(CUSTOMER_SESSION_COOKIE)?.value;
}

/**
 * CustomerSession is which email the visitor is signed in as, as the API reports
 * it. `ticket_sale_id` is null for a full Customer Session spanning every Ticket
 * Sale the Customer owns; a Confirmation Link session names the one sale it was
 * minted for.
 */
export type CustomerSession = {
  email: string;
  first_name: string;
  last_name: string;
  /**
   * The Customer's stored Tax ID, both halves null until they have supplied one.
   * The checkout dialog prefills from these beside the email and name (#98).
   */
  tax_id_type: string | null;
  tax_id_number: string | null;
  /**
   * The Customer's stored phone number in canonical E.164 form, null until they
   * have one. The checkout dialog splits it back into a country selection and a
   * national number and prefills the field from it, which is what stops a
   * returning buyer typing their number a second time (#108).
   */
  phone: string | null;
  /**
   * The Customer's Avatar as a browser-loadable URL, null when they have none —
   * the header menu and "My info" render initials in its place.
   */
  avatar_url: string | null;
  verified_at: string | null;
  ticket_sale_id: string | null;
};

export type CustomerVerifyResult = {
  session: CustomerSession;
  session_id: string;
};

/** One Ticket Type and the quantity bought within a Ticket Sale. */
export type TicketSaleLine = {
  ticket_type_name: string;
  quantity: number;
  unit_price_cents: number;
};

/**
 * TicketSale is one of the Customer's purchases as the Customer Area shows it:
 * the Event and its date, the Organization that sold it, what was bought, and
 * the Sale Confirmation reference they can quote to a promoter.
 */
export type TicketSale = {
  id: string;
  confirmation_ref: string;
  sold_at: string;
  status: string;
  amount_cents: number;
  currency: string;
  lines: TicketSaleLine[];
  event: {
    id: string;
    name: string;
    slug: string;
    starts_at: string | null;
    ends_at: string | null;
    timezone: string | null;
    venue_name: string | null;
  };
  organization: {
    id: string;
    name: string;
    slug: string;
  };
  /**
   * The Tax ID this sale was transacted under, both halves null together on a
   * legacy or imported sale that carries none (#99). It is the sale's immutable
   * snapshot, not the Customer's current stored Tax ID: editing "My info" or
   * buying again under a company RUC never rewrites what an old sale shows.
   */
  tax_id_type: string | null;
  tax_id_number: string | null;
  /**
   * Whether the Customer can undo this purchase right now (#119), and the instant
   * the offer expires — the earlier of 20:00 Ecuador time on the day of purchase
   * or the Event's start.
   *
   * `reversible` is the API's own answer to the question the undo endpoint asks
   * itself, so this app never has to reason about who may undo what: it draws the
   * action where the API says yes and nowhere else. Three things go into it — an
   * active Online Sale, an open Reversal Window, and a payment that can actually
   * be undone. The last is why a paid purchase inside its window can come back
   * false while a free claim comes back true.
   *
   * The two fields answer together: `reversible_until` is null whenever
   * `reversible` is false, so there is no closed deadline for this app to draw by
   * mistake. It is named for the offer and not for the Reversal Window, because
   * it is the deadline on the former: a sale whose payment cannot be reversed has
   * an open Window and no offer, and reports null.
   *
   * It is a reading of one instant, not a promise. The API re-checks everything
   * on the request, so a stale true here becomes a refusal with a message rather
   * than a reversal that should not have happened.
   */
  reversible: boolean;
  reversible_until: string | null;
};

/**
 * What the API returns when a purchase has been undone: the sale, still carrying
 * the Sale Confirmation reference from the buyer's receipt, and when it went.
 *
 * The reference survives because the sale does. A reversed Ticket Sale is never
 * deleted — it keeps its reference and stays in the Customer Area with a
 * Reversed badge — so the thing someone would quote to an organizer reads the
 * same before and after.
 */
export type TicketSaleReversal = {
  ticket_sale_id: string;
  confirmation_ref: string;
  status: string;
  reversed_at: string;
};

/** The Customer Area read: purchases split into what is still to come and what has happened. */
export type CustomerArea = {
  upcoming: TicketSale[];
  past: TicketSale[];
};

/**
 * SessionOutcome distinguishes the three things a session read can mean, because
 * the Storefront treats them differently: a signed-in visitor sees their Area, a
 * visitor whose session is gone is sent to sign-in rather than shown an error
 * (PRD user story 37), and a transport failure must not masquerade as either.
 */
export type SessionOutcome<T> =
  | { status: "ok"; data: T }
  | { status: "signed-out" }
  /**
   * `code` is the API's own code when the failure came back as an envelope, and
   * null when nothing was reached at all. It travels beside the message so the
   * page rendering this can choose its copy on the code and fall back to the
   * message (ADR 0023) — the same pair every other failed call hands its
   * surface.
   *
   * Both are null when the API was never reached. The fallback is only ever the
   * API's OWN words; a thrown transport error's `message` is not those words —
   * it is a runtime string ("fetch failed"), written in English by a library
   * with no idea a Customer will read it. Handing that to the surface as if it
   * were copy is what put an English sentence on a Spanish page. Null instead,
   * so the surface says its own translated sentence.
   */
  | { status: "error"; code: string | null; message: string | null };

/**
 * signedOutStatuses are the API responses that mean "this token is worth
 * nothing": expired, destroyed by a sign-out, or never valid. Any of them sends
 * the visitor to sign-in.
 */
function isSignedOut(error: unknown): boolean {
  return error instanceof APIError && error.status === 401;
}

async function readWithSession<T>(path: string): Promise<SessionOutcome<T>> {
  const token = await customerSessionToken();
  if (!token) {
    // No cookie at all: the common case for every anonymous visitor, and it
    // costs exactly nothing — no call to the API is made.
    return { status: "signed-out" };
  }

  try {
    const envelope = await callBackend<T>(path, { method: "GET", sessionToken: token });
    if (!envelope.data) {
      return { status: "signed-out" };
    }
    return { status: "ok", data: envelope.data };
  } catch (error) {
    if (isSignedOut(error)) {
      return { status: "signed-out" };
    }
    // Only an APIError carries an envelope, and only an envelope carries words
    // meant for a Customer. Everything else reached nothing and has nothing to
    // relay.
    return error instanceof APIError
      ? { status: "error", code: error.code, message: error.message }
      : { status: "error", code: null, message: null };
  }
}

/** Reads the current Customer Session, extending its sliding window as a side effect. */
export async function getCustomerSession(): Promise<SessionOutcome<CustomerSession>> {
  return readWithSession<CustomerSession>("/api/v1/customer/auth/session");
}

/**
 * Reads the Customer Area.
 *
 * The request carries no Customer, email, or Organization — only the session
 * token. The API scopes the result to the Customer on that session and to
 * nothing else, so there is no identifier here that could widen it.
 */
export async function getCustomerArea(): Promise<SessionOutcome<CustomerArea>> {
  return readWithSession<CustomerArea>("/api/v1/customer/ticket-sales");
}
