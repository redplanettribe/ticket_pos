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
import type { ConsentBoxes } from "./checkout-consent";

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
  return {
    ...customerSessionCookieOptions(),
    maxAge: CONFIRMATION_LINK_SESSION_MAX_AGE_SECONDS,
  };
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
  /**
   * Which consent boxes a capture surface must still show this Customer (#254).
   *
   * True means OUTSTANDING — not answered, and therefore to be ASKED. It never
   * means "already agreed": every box is drawn unticked, always, because consent
   * has to be something the person actively gave.
   *
   * It rides on the session read rather than on a read of its own so that the
   * checkout dialog gets it in the same response as the email and Tax ID it
   * prefills from: who is buying and what may still be asked of them are one
   * snapshot of one session, and two reads could disagree. Behind a session it
   * is no oracle either — it only ever tells a Customer about themselves.
   *
   * A Confirmation Link session (`ticket_sale_id` set) reports all three true
   * whatever is stored, because a forwarded email proves nothing about who is
   * holding it and the checkout treats such a session as a guest's.
   */
  consent_boxes: ConsentBoxes;
};

/**
 * What a sign-in door answers with, and it has TWO SHAPES (#251).
 *
 * Either the address was proven and the Customer has accepted the Policy
 * Version in effect — `session` and `session_id` are set — or the address was
 * proven and they have not, in which case both are null and `consent_required`
 * carries the pending-consent token and the boxes to show. Proof succeeded
 * either way: a missing `session_id` here is a consent step, never a failure,
 * and a route that treats it as one reports a working sign-in as broken.
 */
export type CustomerVerifyResult = {
  session: CustomerSession | null;
  session_id: string | null;
  consent_required: ConsentRequired | null;
};

/**
 * The consent step: what the Storefront must show before this sign-in can
 * finish, and the credential that finishes it.
 *
 * The token is short-lived and single-use, and it is the only thing this app
 * ever holds between the passcode and the session. It never reaches page
 * scripts as a session would — it buys nothing but a consent submission — but
 * it is handed to the form, because the form is what collects the answers it is
 * exchanged for.
 *
 * `boxes` says which checkboxes to render, and says nothing about what to
 * pre-tick: every box is drawn UNTICKED, always. A box the API did not ask for
 * is one this Customer has already answered, and re-showing it would churn a
 * standing answer.
 */
export type ConsentRequired = {
  pending_consent_token: string;
  expires_at: string;
  boxes: {
    policy_acceptance: boolean;
    marketing_consent: boolean;
    networking_consent: boolean;
    /**
     * The Terms box (#536, ADR 0066): the other REQUIRED box, owed
     * independently of the policy's because the two documents version
     * independently. On a consent-required outcome at least one of the two
     * required boxes is true.
     */
    terms_acceptance: boolean;
  };
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
  /**
   * Whether a Reversal Request for this sale is in flight: the Customer asked to
   * undo it, the Payment Provider gave an answer nobody can act on, and the
   * platform is still finding out what happened to the money (ADR 0024).
   *
   * It says nothing about the tickets, which is the whole point. The sale stays
   * `active`, its capacity stays held and these tickets stay valid for entry,
   * because no money is known to have moved — so the card that reads this must
   * say a refund is being processed and must not dim, strike through or
   * otherwise imply the purchase is gone.
   *
   * It is also what withdraws the Undo action while the answer is unknown; the
   * card composes that (components/ticket-sale-card.tsx).
   */
  reversal_pending: boolean;
  /**
   * The state of the live Reversal Request over this sale, straight from
   * `sale_reversals.status`, and null when the sale has none (ADR 0024).
   *
   * `reversal_pending` answers "is a refund being worked on"; this answers "how
   * did it end", and the two questions are not the same one asked twice. A
   * request that was refused and one that became an Unresolved Reversal both
   * leave the sale `active` and `reversal_pending` false, so nothing but this
   * field can tell them apart — and they are opposite things to say to a
   * Customer: the first means their money never moved and is not coming, the
   * second means nobody yet knows where it is.
   *
   * It is carried as the request's own four states rather than as a flag per
   * outcome so that a surface which must say nothing for some of them can spell
   * out which ones, instead of a new state arriving as the absence of every flag
   * and inheriting whichever branch was written for "not the good one".
   */
  reversal_status: ReversalRequestStatus | null;
};

/**
 * The four states a Reversal Request ends up in (ADR 0024), named here exactly as
 * the API sends them.
 *
 * `needs_attention` is an Unresolved Reversal: the platform gave up with the
 * money's fate unknown and a Platform Operator settles it. Nothing about it may
 * be reported to the Customer, so it is spelled out rather than left to fall in
 * with the rest.
 */
export type ReversalRequestStatus =
  "in_flight" | "succeeded" | "refused" | "needs_attention";

/**
 * What the API returns when a purchase has been undone: the sale, still carrying
 * the Sale Confirmation reference from the buyer's receipt, and when it went.
 *
 * The reference survives because the sale does. A reversed Ticket Sale is never
 * deleted — it keeps its reference and stays in the Customer Area with a
 * Reversed badge — so the thing someone would quote to an organizer reads the
 * same before and after.
 */
export type TicketSaleReversed = {
  ticket_sale_id: string;
  confirmation_ref: string;
  status: "reversed";
  /** When the platform learned the reversal had succeeded. */
  reversed_at: string;
};

/**
 * What the API returns when the undo could not be answered: a Reversal Request
 * is in flight, the Payment Provider gave an outcome nobody can act on, and the
 * platform will pursue it to a definite answer (ADR 0024).
 *
 * The timestamp is `requested_at` and not `reversed_at`, and the difference is
 * the entire point of this type existing: it is when the Customer pressed, not
 * when anything happened to their money — because whether anything did is
 * exactly what is unknown. A second press while the request is in flight is a
 * read of the same row and returns this same instant, not a new one.
 */
export type TicketSaleReversalPending = {
  ticket_sale_id: string;
  confirmation_ref: string;
  status: "pending";
  /** When the Customer asked, carried by the one live Reversal Request. */
  requested_at: string;
};

/**
 * The two ways an undo can succeed as an HTTP call, which are not the two ways
 * it can end for the Customer. `status` is the discriminator and the only thing
 * any surface may conclude "done" from: a body that is not `reversed` has not
 * reversed anything, whatever status code carried it.
 */
export type TicketSaleReversal = TicketSaleReversed | TicketSaleReversalPending;

/** The Customer Area read: purchases split into what is still to come and what has happened. */
/**
 * One Ticket this Customer HOLDS rather than bought: an Event somebody else paid
 * for, assigned to their address, and which they accepted (#325, ADR 0046).
 *
 * A SEPARATE SHAPE FROM TicketSale, AND THE SEPARATION IS THE DISCLOSURE RULE. A
 * Holder is not the party of record — the money, the Sale Confirmation, the
 * confirmation reference and the Reversal Window all stayed with the buyer — so
 * there is no amount here, no reference, no Tax ID, no reversal state and no
 * Undo. The API does not send them; this type is why nothing here can start
 * drawing them.
 */
export type HeldTicket = {
  ticket_id: string;
  accepted_at: string;
  ticket_type_name: string;
  // The same two shapes a TicketSale carries, written out rather than shared,
  // because sharing them would put this type one refactor away from carrying the
  // rest of a sale.
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
};

export type CustomerArea = {
  upcoming: TicketSale[];
  past: TicketSale[];
  /**
   * The Tickets somebody gave this Customer and they accepted. Empty for almost
   * everybody, and empty for a Confirmation Link session by construction: that
   * credential opens one Ticket Sale, and a Ticket held on somebody else's sale
   * is not it.
   */
  holding: HeldTicket[];
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
    const envelope = await callBackend<T>(path, {
      method: "GET",
      sessionToken: token,
    });
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
export async function getCustomerSession(): Promise<
  SessionOutcome<CustomerSession>
> {
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

/**
 * The Organization as a Follow reports it: the same three facts its public
 * profile publishes. No id — a Follow is addressed by slug everywhere, on the
 * API and in this app's own addresses.
 */
export type FollowedOrganization = {
  name: string;
  slug: string;
  logo_url: string | null;
};

/**
 * The Tag as a Follow reports it: the same three facts every other Tag surface
 * in this app receives (`PublicTag` in lib/api.ts).
 *
 * It is structurally that same shape on purpose — `tagName()` takes it as-is, so
 * a Followed Preset Tag is worded from this app's own message catalogue and a
 * Custom Tag is rendered exactly as its Organization coined it, by the one
 * helper that decides that everywhere else (ADR 0027). `curated` is what tells
 * the two apart; it says nothing about whether a Tag may be Followed, since
 * every Tag may (ADR 0030).
 */
export type FollowedTag = {
  canonical_key: string;
  name: string;
  curated: boolean;
};

/**
 * One of the Customer's Follows.
 *
 * `type` is the discriminator and the subject hangs off the field it names, so
 * the two kinds cannot collide and a consumer narrows with a plain `switch`.
 * They arrive interleaved in one list from one endpoint — there is no second
 * read to union and no second order to reconcile.
 */
export type Follow =
  | {
      type: "organization";
      /**
       * When the Customer subscribed. Stable across repeats: following something
       * already followed returns this instant rather than a new one, so the
       * list's order does not shuffle under a double tap.
       */
      followed_at: string;
      organization: FollowedOrganization;
    }
  | {
      type: "tag";
      followed_at: string;
      tag: FollowedTag;
    };

/** Everything the Customer Follows, most recently followed first. */
export type Follows = {
  follows: Follow[];
  /**
   * Whether the weekly Follow Digest is switched on for this Customer (#224).
   *
   * It rides on the Follows listing rather than on a read of its own because the
   * Customer Area has one sentence to say — the digest is off, and everything
   * you follow still stands — and two reads that could disagree is how a page
   * comes to show a full list beside a switch that has not caught up.
   *
   * Unsubscribing and Unfollowing are different acts (CONTEXT.md): this is the
   * only one that changes, and no Follow above it moves either way.
   */
  digest_enabled: boolean;
};

/**
 * Reads everything the signed-in Customer Follows.
 *
 * Like the Customer Area, the request carries no identifier of whose list it is:
 * the session is the only scope. An anonymous visitor never reaches the API at
 * all — no cookie, no call — so a page that asks this in order to decide how to
 * draw a Follow control costs a signed-out reader nothing.
 *
 * One read answers every Follow control on a page, which is why it is a list
 * rather than a per-subject "do I follow this" probe: a page showing several
 * followable things asks once.
 */
export async function getFollows(): Promise<SessionOutcome<Follows>> {
  return readWithSession<Follows>("/api/v1/customer/follows");
}

/**
 * Why a subject is being suggested: the Tag that produced it (#231, #232, #234,
 * ADR 0031).
 *
 * NULL WHENEVER THE SUBJECT WAS RANKED ON ACTIVITY — the count of discoverable
 * upcoming Events behind it — which is every suggestion made to a Customer who
 * Follows no Tag, and the tail of the panel for one who does. Activity has no
 * producing Tag to name.
 *
 * THE TAG ITSELF, IN THE SAME SHAPE AS EVERY OTHER TAG ON THE WIRE, and it is
 * self-sufficient on purpose (#234). It used to be a canonical key alone and the
 * panel went to the Follows listing for the name and the `curated` flag needed
 * to word it — which holds Tags the Customer Follows DIRECTLY, so once a reason
 * could name a DERIVED Tag (#233, never in that listing by ADR 0031) the lookup
 * missed and the line vanished. Nothing about wording a reason may depend on a
 * second read.
 *
 * Still no sentence and no Locale on the wire: `tagName()` words it from this
 * app's own catalogues exactly as it words the subject beside it (ADR 0027), and
 * `name` is the English fallback for the Custom Tags no catalogue can hold.
 */
export type SuggestionReason = {
  tag: FollowedTag;
};

/** One Tag the Customer does not Follow, offered as one they might. */
export type SuggestedTag = {
  /**
   * The subject, in the SAME shape a followed Tag arrives in. That is the point
   * of the API publishing it this way: `tagName()` takes it as-is and this app
   * holds one type per followable kind, so a suggested Tag and a followed Tag
   * are drawn from the same catalogue in the same words.
   */
  tag: FollowedTag;
  reason: SuggestionReason | null;
};

/** One Organization the Customer does not Follow, in a followed one's shape. */
export type SuggestedOrganization = {
  organization: FollowedOrganization;
  reason: SuggestionReason | null;
};

/**
 * The Suggested Follows panel: TWO GROUPS, never one interleaved list.
 *
 * The opposite of the Follows listing above, deliberately. That one interleaves
 * because `followed_at` is one real scale across both kinds; here the two kinds
 * are ranked over different populations, so ordering them together would invent
 * a comparability that does not exist. The presentation splits them anyway —
 * chips for Tags, rows for Organizations — so a reader knows which kind of thing
 * is on offer before reading a word.
 *
 * Both arrive as arrays and are empty rather than absent when nothing qualifies:
 * a Customer who already Follows everything worth Following is this feature
 * working, not failing.
 */
export type FollowSuggestions = {
  tags: SuggestedTag[];
  organizations: SuggestedOrganization[];
};

/**
 * Reads the Suggested Follows for the signed-in Customer.
 *
 * A SECOND READ, made server-side beside getFollows() on the Following page and
 * nowhere else. It is not folded into the Follows listing because that listing
 * resolves the Follow control on the explorer and on every Event and
 * Organization page, and those pages must not pay for a ranking they discard
 * (ADR 0031).
 *
 * There is no route handler under app/api/customer/ for this, and that absence
 * is the design rather than an omission: the route handlers exist for controls
 * the BROWSER operates, and nothing in the panel is operated by the browser
 * except the Follow control, which already has one and is reused unchanged.
 *
 * The failure posture is the caller's to apply and is stated where the panel is
 * rendered: a failed read means NO PANEL, silently. Nobody arrives at the
 * Following page for suggestions, so an error banner about a feature the reader
 * never asked for is worse than its absence.
 */
export async function getFollowSuggestions(): Promise<
  SessionOutcome<FollowSuggestions>
> {
  return readWithSession<FollowSuggestions>(
    "/api/v1/customer/follow-suggestions",
  );
}

/**
 * One optional consent's state, as the API reports it (#268, parent #265).
 *
 * FOUR VALUES, AND THEY MEAN FOUR DIFFERENT THINGS. Two of them are not answers
 * at all: `unanswered` is a question this Customer was never asked, and
 * `pending_confirmation` is somebody else's tick — a guest typed this address at
 * a checkout and ticked the box, so the platform is holding it unresolved
 * (ADR 0035). Neither may be drawn as a refusal. A boolean here would collapse
 * three of the four into "off" and have the page tell a Customer they had
 * declined something nobody ever asked them about.
 *
 * `unanswered` exists on the wire only. The platform stores NULL, because the
 * absence of an answer is not a fourth kind of answer.
 */
export type ConsentState =
  "granted" | "denied" | "pending_confirmation" | "unanswered";

/** The two optional consents, which are the only two a Customer can move. */
export type OptionalConsents = {
  marketing_consent: ConsentState;
  networking_consent: ConsentState;
};

/**
 * The Privacy page's read: what this Customer has authorized.
 *
 * The Policy Version is THE ONE THEY ACCEPTED and not the one in effect, so a
 * Customer who accepted a superseded edition is told what they actually agreed
 * to. Both policy fields are null together where no acceptance was ever
 * recorded, and neither has a control beside it: Policy Acceptance is not
 * withdrawable.
 */
export type Privacy = {
  policy_version: string | null;
  policy_accepted_at: string | null;
  consents: OptionalConsents;
};

/**
 * What one Withdraw All did (#269).
 *
 * IT REPORTS TWO DIFFERENT FACTS AND BOTH ARE NEEDED. `consents` is what is
 * true now; `withdrew` is what the act actually TOOK AWAY, decided by the API
 * inside the transaction that observed the prior state. They come apart exactly
 * where it matters: a Customer whose consents were both already denied ends in
 * the same states as one who just gave two things up, and only the second is
 * sent a confirmation email — so a page reading the states alone would promise
 * mail that the API correctly did not send.
 */
export type WithdrawAll = {
  consents: OptionalConsents;
  withdrew: { marketing_consent: boolean; networking_consent: boolean };
};

/**
 * Reads the signed-in Customer's privacy settings.
 *
 * A GET, AND THAT IS THE POINT. Rendering the Privacy page must write nothing:
 * a settings surface that recorded a refusal because somebody read it would
 * convert "never asked" into "denied" for everybody who opened it and did
 * nothing. Only moving a control writes anything, and that goes through the BFF
 * route beside this, never through here.
 *
 * Like every other Customer Area read, the request carries no identifier of
 * whose settings these are: the session is the only scope.
 */
export async function getPrivacy(): Promise<SessionOutcome<Privacy>> {
  return readWithSession<Privacy>("/api/v1/customer/privacy");
}
