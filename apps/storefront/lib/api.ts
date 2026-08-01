import type { ReversalOffer } from "./undo-window";

export type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
  request_id?: string;
};

function apiBaseUrl(): string {
  const url = process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return url.replace(/\/$/, "");
}

// --- Service-to-service authentication (ADR 0008) -------------------------
//
// The Go API is deployed with unauthenticated invocation disabled, so every
// server-side call from this app must carry a Google-signed OIDC ID token
// naming the API service URL as its audience, fetched from the Cloud Run
// metadata server.
//
// Locally there is no metadata server. We detect the platform from K_SERVICE,
// which Cloud Run always sets, rather than probing the metadata address — a
// probe to a link-local address that nothing answers can stall for a long time.

const METADATA_TOKEN_URL =
  "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity";
const METADATA_TIMEOUT_MS = 5_000;
// Refresh a little before expiry so an in-flight request never carries a token
// that expires mid-flight.
const TOKEN_EXPIRY_MARGIN_MS = 60_000;
// Google ID tokens live one hour; used only if the token has no readable exp.
const TOKEN_FALLBACK_TTL_MS = 45 * 60_000;

/**
 * ServiceAuthError marks a failure to obtain this service's own credential.
 * It is thrown rather than swallowed: an unauthenticated call would be
 * rejected by Cloud Run with a confusing 403 that looks like an app bug.
 */
export class ServiceAuthError extends Error {
  constructor(audience: string, cause: unknown) {
    super(`Failed to obtain an OIDC ID token for ${audience}: ${String(cause)}`);
    this.name = "ServiceAuthError";
    this.cause = cause;
  }
}

let cachedToken: { token: string; expiresAtMs: number } | null = null;
let inflightToken: Promise<string> | null = null;

function runningOnCloudRun(): boolean {
  return Boolean(process.env.K_SERVICE);
}

function tokenExpiryMs(token: string): number {
  try {
    // base64url -> base64; atob exists in every runtime this module can be
    // bundled for, Buffer does not.
    const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    const claims = JSON.parse(atob(payload)) as { exp?: number };
    if (typeof claims.exp === "number") {
      return claims.exp * 1000;
    }
  } catch {
    // fall through to the conservative default
  }
  return Date.now() + TOKEN_FALLBACK_TTL_MS;
}

async function requestIdToken(audience: string): Promise<string> {
  const url = `${METADATA_TOKEN_URL}?audience=${encodeURIComponent(audience)}`;
  const response = await fetch(url, {
    headers: { "Metadata-Flavor": "Google" },
    cache: "no-store",
    signal: AbortSignal.timeout(METADATA_TIMEOUT_MS),
  });
  if (!response.ok) {
    throw new Error(`metadata server responded ${response.status}`);
  }
  const token = (await response.text()).trim();
  if (!token) {
    throw new Error("metadata server returned an empty token");
  }
  cachedToken = { token, expiresAtMs: tokenExpiryMs(token) };
  return token;
}

/**
 * serviceIdToken returns the ID token to present to the API, or null when this
 * process is not running on Cloud Run (local development, tests, the parity
 * stack), where the API accepts unauthenticated callers. Off-platform it costs
 * nothing: no network call is attempted.
 */
async function serviceIdToken(): Promise<string | null> {
  if (!runningOnCloudRun()) {
    return null;
  }
  if (cachedToken && cachedToken.expiresAtMs - TOKEN_EXPIRY_MARGIN_MS > Date.now()) {
    return cachedToken.token;
  }
  const audience = apiBaseUrl();
  inflightToken ??= requestIdToken(audience).finally(() => {
    inflightToken = null;
  });
  try {
    return await inflightToken;
  } catch (error) {
    throw new ServiceAuthError(audience, error);
  }
}

/**
 * serviceAuthHeaders builds the headers carrying this service's credential.
 *
 * The token goes in X-Serverless-Authorization, not Authorization: Cloud Run
 * checks that header for IAM when present and strips it before the container
 * sees the request, leaving Authorization untouched for end-user credentials.
 */
async function serviceAuthHeaders(): Promise<HeadersInit | undefined> {
  const token = await serviceIdToken();
  return token ? { "X-Serverless-Authorization": `Bearer ${token}` } : undefined;
}

/**
 * APIError carries a failed envelope from the Go API so a route handler can
 * relay the API's own `error.code` and `error.message` to the browser instead of
 * inventing a verdict of its own (docs/design/README.md). The code is the half
 * the rendering surface keys its copy on, and the message is the half it falls
 * back to when it does not know the code (ADR 0023) — both have to survive.
 */
export class APIError extends Error {
  code: string;
  details?: unknown;
  requestId: string;
  status: number;

  constructor(status: number, error: NonNullable<APIEnvelope<unknown>["error"]>, requestId: string) {
    super(error.message);
    this.code = error.code;
    this.details = error.details;
    this.requestId = requestId;
    this.status = status;
  }
}

/**
 * What a successful call to the Go API came back as: the envelope, plus the HTTP
 * status that carried it.
 *
 * The status is here because "the call succeeded" stopped being one thing. A
 * Customer's undo now answers 200 when the Sale Reversal is done and 202 when a
 * Reversal Request is in flight and nobody yet knows whether the money moved
 * (ADR 0024), and both are `response.ok`. A relay that only forwarded the body
 * would flatten the two into the same answer, and the one it would flatten them
 * into is "your purchase is undone" — a claim about somebody's money that no
 * part of the system has made.
 *
 * `status` describes the call, not the payload, so a BFF route relaying this to
 * the browser names the envelope's three fields rather than spreading this whole
 * object: spreading it would put a `status` key inside the response body, where
 * the envelope contract has never had one and a client reading `data.status`
 * beside it would have two unrelated things spelled the same.
 */
export type BackendResponse<T> = APIEnvelope<T> & {
  /** The API's own HTTP status. Only successful ones reach here; failures throw. */
  status: number;
};

/**
 * callBackend proxies one request to the Go API and returns its envelope,
 * throwing APIError when the call failed.
 *
 * It is the counterpart of the read-only fetchData below: fetchData degrades a
 * failure to an empty page, which is right for anonymous browsing and wrong for
 * sign-in, where the visitor must be told exactly why a passcode was rejected.
 *
 * `sessionToken` is the Customer Session token held in this app's httpOnly
 * cookie. It goes in Authorization; the service credential goes in
 * X-Serverless-Authorization, and the two never displace each other (ADR 0008).
 */
export async function callBackend<T>(
  path: string,
  init: RequestInit & { sessionToken?: string } = {},
): Promise<BackendResponse<T>> {
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  if (init.sessionToken) {
    headers.set("Authorization", `Bearer ${init.sessionToken}`);
  }
  if (!headers.has("X-Request-ID")) {
    headers.set("X-Request-ID", crypto.randomUUID());
  }
  const serviceHeaders = new Headers(await serviceAuthHeaders());
  serviceHeaders.forEach((value, key) => headers.set(key, value));

  const response = await fetch(`${apiBaseUrl()}${path}`, {
    ...init,
    cache: "no-store",
    headers,
  });

  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new APIError(
      response.status,
      envelope.error ?? { code: "INTERNAL_ERROR", message: "Request failed" },
      envelope.request_id ?? crypto.randomUUID(),
    );
  }
  return { ...envelope, status: response.status };
}

export type PublicOrganization = {
  name: string;
  slug: string;
  logo_url: string | null;
};

export type PublicOrganizationSummary = {
  name: string;
  slug: string;
  logo_url: string | null;
  // The Organization's Support WhatsApp number in canonical E.164 form — the
  // number a Customer messages for help (ADR 0027).
  //
  // Present ONLY on the Event detail, and optional even there: the API omits the
  // key entirely for an Organization that has set no number. It is deliberately
  // absent from every listing payload — the explorer cards and the Organization
  // page — so a paginated public endpoint cannot be harvested for every
  // Organization's support number at once. Hence `?` rather than `| null`: the
  // key's absence is the contract, on two counts at once.
  support_whatsapp?: string;
};

export type PublicEventCard = {
  slug: string;
  name: string;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string | null;
  venue_name: string | null;
  cover_image_url: string | null;
  organization: PublicOrganizationSummary;
  currency: string;
  price_from_cents: number | null;
  sold_out: boolean;
  tags: PublicTag[];
  // How the Event takes sign-ups, "tickets" or "external"; see
  // lib/registration.ts, which reads it so the cards do not have to. A card
  // needs it because a null price_from_cents cannot be read on its own: on a
  // ticketed Event it is an anomaly worth no words, and on an external one it is
  // the normal state and gets them.
  registration_mode: string;
};

// A live Promotion on a Ticket Type: the Promotional Price that overrides the
// List Price until the window closes (ADR 0021). Present only while the window
// is live — the API omits it before it opens and once it has ended, so this app
// never evaluates the window itself. Both amounts are buyer prices on the same
// footing as price_cents, fee included where the Event passes it on.
export type PublicPromotion = {
  // Equal to the Ticket Type's price_cents while the Promotion is live.
  promotional_price_cents: number;
  // What the ticket costs once the Promotion ends: the price to strike through.
  list_price_cents: number;
  // RFC3339 instant; rendered in the Event's timezone.
  ends_at: string;
};

export type PublicTicketType = {
  // The Ticket Type's id, which begin-checkout lines are keyed by.
  id: string;
  name: string;
  description: string | null;
  // The effective buyer price, server-computed: what checkout will charge per
  // ticket, already carrying the service fee where the Event passes it on, and
  // already the Promotional Price where a Promotion is live. This app never
  // computes fees and never applies a Promotion to it — it shows this number
  // (ADR 0014, ADR 0021).
  price_cents: number;
  currency: string;
  remaining: number;
  sold_out: boolean;
  promotion: PublicPromotion | null;
  // The Purchase Limit: the most of this Ticket Type one Customer may hold at
  // once, or null when it is unrestricted (ADR 0025). A raw count of tickets,
  // untouched by the fee and Promotion arithmetic price_cents carries.
  //
  // It states the Ticket Type's rule and nothing about any Customer's holdings,
  // which is why the anonymous Event page can be bounded by it at all. The
  // quantity steppers narrow it further by already_held below, via
  // offerableQuantity.
  max_per_customer: number | null;
  // How many of this Ticket Type the Customer who asked for this page already
  // holds: their active Ticket Sales plus their live Capacity Holds, the same
  // count begin-checkout refuses on and under the same name its
  // PURCHASE_LIMIT_EXCEEDED details use (ADR 0025, #168).
  //
  // Null for an anonymous read, and that is not the same statement as 0: null
  // says we do not know who is asking, zero says this Customer holds none. The
  // API fills it for every Ticket Type a signed-in Customer reads, unrestricted
  // ones included, so null never doubles as "unrestricted" — max_per_customer
  // already answers that question.
  //
  // It arrives only when getPublicEvent carries the Customer Session token, and
  // it may legitimately exceed max_per_customer, because lowering a Purchase
  // Limit is not retroactive.
  already_held: number | null;
};

export type PublicEventDetail = {
  slug: string;
  name: string;
  description: string | null;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string | null;
  venue_name: string | null;
  venue_address: string | null;
  cover_image_url: string | null;
  // The Event's optional Cover Video, played in the hero over the cover image
  // poster. Listings and link previews stay on the cover image.
  cover_video_url: string | null;
  has_ended: boolean;
  organization: PublicOrganizationSummary;
  currency: string;
  // Whether the quoted prices carry the platform's service fee, and therefore
  // whether the muted "includes service fee" note is shown. Under absorb the
  // buyer pays exactly what the organizer set and no fee is mentioned at all.
  price_includes_fee: boolean;
  ticket_types: PublicTicketType[];
  tags: PublicTag[];
  // Whether the Event advertises itself. It gates nothing about rendering or
  // selling — a published Event is reachable by direct link either way (ADR
  // 0002) — and is read only by generateMetadata, which marks a
  // non-Discoverable Event noindex so a leaked URL cannot put it in a search
  // result the organizer opted out of.
  discoverable: boolean;
  // How this Event takes sign-ups: "tickets" (it sells Ticket Types here) or
  // "external" (it hands its audience to the Registration Link). Never both
  // (ADR 0028). It is what decides whether the page renders a tickets section at
  // all; see lib/registration.ts, which reads it so the page does not have to.
  registration_mode: string;
  // The Registration Link of an external Event, and null on a ticketed one. The
  // page needs the URL itself and not merely the fact of it: the Register panel
  // names the destination's hostname beneath the call to action.
  registration_url: string | null;
};

export type PublicEventPage = {
  events: PublicEventCard[];
  next_cursor: string | null;
};

export type PublicOrganizationEvents = {
  organization: PublicOrganizationSummary;
  upcoming: PublicEventCard[];
  past: PublicEventCard[];
};

// Shared page size for the global explorer so the initial SSR page and each
// client "Load more" fetch request the same number of cards.
export const EXPLORER_PAGE_SIZE = 12;

export type PublicTag = {
  // The Tag's stable machine identity, and what a Preset Tag's Locale copy is
  // keyed on (ADR 0027). Lowercased, and never containing a "."; `name` is the
  // English display name it falls back to.
  canonical_key: string;
  name: string;
  curated: boolean;
};

export type ListEventsParams = {
  q?: string;
  from?: string;
  to?: string;
  tags?: string[];
  cursor?: string;
  limit?: number;
};

/**
 * Opting one read into Next's Data Cache.
 *
 * Every read here is `cache: "no-store"` by default and stays that way: an
 * Event page shows remaining stock and a live Promotional Price, and a buyer
 * looking at a cached count is a buyer being lied to.
 *
 * `revalidate` is for the one caller that wants the opposite — a route that is
 * itself regenerated on a timer and whose reads must be allowed to live that
 * long (app/sitemap.ts). It matters that this is opt-IN and per call: `cache:
 * "no-store"` on the fetch OVERRIDES a route's own `export const revalidate`,
 * so a route asking to be regenerated hourly while calling an uncached fetch
 * quietly regenerates on every single request. Nothing about that failure is
 * visible — the page is correct, it is just never cached — which is why the two
 * options are mutually exclusive below rather than merged.
 */
export type ReadCache = {
  /** Seconds this read may be served from the Data Cache. */
  revalidate: number;
};

function cacheInit(cache?: ReadCache): RequestInit {
  return cache ? { next: { revalidate: cache.revalidate } } : { cache: "no-store" };
}

async function fetchData<T>(
  path: string,
  cache?: ReadCache,
  sessionToken?: string,
): Promise<T | null> {
  // Deliberately outside the try: a missing service credential is a
  // deployment fault and must surface, while an API that is merely down or
  // unhappy still degrades to an empty page.
  const headers = new Headers(await serviceAuthHeaders());
  // The Customer Session token, on a read that is public either way. It goes in
  // Authorization while the service credential stays in
  // X-Serverless-Authorization, exactly as callBackend arranges them, so neither
  // ever displaces the other (ADR 0008).
  if (sessionToken) {
    headers.set("Authorization", `Bearer ${sessionToken}`);
  }
  try {
    const response = await fetch(`${apiBaseUrl()}${path}`, { ...cacheInit(cache), headers });
    const envelope = (await response.json()) as APIEnvelope<T>;
    if (!response.ok || envelope.error) {
      return null;
    }
    return envelope.data;
  } catch {
    return null;
  }
}

export async function getPublicOrganization(slug: string): Promise<PublicOrganization | null> {
  return fetchData<PublicOrganization>(`/api/v1/public/organizations/${encodeURIComponent(slug)}`);
}

export async function listPublicEvents(
  params: ListEventsParams = {},
  cache?: ReadCache,
): Promise<PublicEventPage | null> {
  const query = new URLSearchParams();
  if (params.q) query.set("q", params.q);
  if (params.from) query.set("from", params.from);
  if (params.to) query.set("to", params.to);
  if (params.tags && params.tags.length > 0) query.set("tags", params.tags.join(","));
  if (params.cursor) query.set("cursor", params.cursor);
  if (params.limit) query.set("limit", String(params.limit));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return fetchData<PublicEventPage>(`/api/v1/public/events${suffix}`, cache);
}

/** How long the sitemap's Event walk may be reused; see listSitemapEvents. */
export const SITEMAP_READ_REVALIDATE_SECONDS = 3600;

/**
 * One page of the sitemap's Event walk — the single read in this app that opts
 * into the Data Cache (see ReadCache above).
 *
 * The opt-in lives here rather than at the call site in app/sitemap.ts because
 * dropping it there would cost nothing visible: the sitemap would still be
 * correct, still be served, and quietly walk the whole catalog again on every
 * crawler hit. A `{ revalidate }` written inside a function is one a unit test
 * can assert (lib/api.test.ts); one written at a call site inside a route is
 * one that has to be trusted.
 *
 * The page size stays with the walk that chose it (SITEMAP_PAGE_SIZE) and is
 * passed in, so this function owns the caching decision and nothing else.
 */
export async function listSitemapEvents(
  cursor: string | undefined,
  limit: number,
): Promise<PublicEventPage | null> {
  return listPublicEvents({ cursor, limit }, { revalidate: SITEMAP_READ_REVALIDATE_SECONDS });
}

// listPublicTags returns the preset filter chips for the explorer. The backend
// derives this from the full discoverable-upcoming pool, so the bar is stable
// regardless of the active q/date/tag selection.
export async function listPublicTags(): Promise<PublicTag[] | null> {
  return fetchData<PublicTag[]>(`/api/v1/public/tags`);
}

export async function getOrganizationEvents(slug: string): Promise<PublicOrganizationEvents | null> {
  return fetchData<PublicOrganizationEvents>(
    `/api/v1/public/organizations/${encodeURIComponent(slug)}/events`,
  );
}

/**
 * The Storefront event page's Event.
 *
 * `sessionToken` is the Customer Session token out of this app's httpOnly
 * cookie, and it changes exactly one thing: each Ticket Type comes back with
 * `already_held`, how many of it the Customer on that session holds, so the
 * steppers can bound themselves at their real remaining allowance and a Ticket
 * Type whose allowance is spent can say so rather than claim to be sold out
 * (ADR 0025, #168). Everything else on the page is identical either way, and a
 * missing, expired or dead token reads the Event as an ordinary visitor — the
 * route is public and never answers 401.
 *
 * It is a parameter rather than a cookie read inside this module because
 * lib/customer-session.ts imports this one; the caller that has already entered
 * a request scope passes it down instead. Passing nothing is the anonymous read,
 * which is what generateMetadata wants: page metadata is the same for everybody
 * and must not vary by who is signed in.
 */
export async function getPublicEvent(
  orgSlug: string,
  eventSlug: string,
  sessionToken?: string,
): Promise<PublicEventDetail | null> {
  return fetchData<PublicEventDetail>(
    `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(eventSlug)}`,
    undefined,
    sessionToken,
  );
}

// --- Online checkout (ADR 0012) --------------------------------------------

/** What the begin-checkout endpoint takes: the requested lines plus the checkout identity. */
export type BeginCheckoutRequest = {
  customer_email: string;
  customer_first_name: string;
  customer_last_name: string;
  /** The buyer's Tax ID, required on this native Sales Channel (ADR 0016). */
  customer_tax_id_type: string;
  customer_tax_id_number: string;
  /**
   * The buyer's phone number in canonical E.164 form, offered to the Payment
   * Provider so its hosted payment page arrives prefilled (#103). Optional in
   * the strongest sense: the key is left off entirely when the buyer gave none,
   * because a value nobody typed is exactly the fabricated cardholder data
   * PayPhone's rules prohibit. Never an empty string, never a placeholder.
   */
  customer_phone?: string;
  /**
   * The Affiliate Link codes this buyer's recent clicks on this Event left
   * behind, newest first, read out of the attribution cookie by the
   * begin-checkout route (ADR 0022). Omitted entirely when nothing is
   * remembered, and never validated here: the API credits the first code that
   * still names a live link and records the sale unattributed when none does —
   * which is why the order, not just the newest code, is what travels.
   */
  affiliate_codes?: string[];
  lines: { ticket_type_id: string; quantity: number }[];
};

/**
 * What begin-checkout returns: our id for the attempt, how it was left, and
 * where to send the Customer next.
 *
 * The two settlements are mutually exclusive and the absent field says which
 * happened. A cart with money to collect is left `pending` and carries the
 * provider's `redirect_url`; one that costs nothing is already `approved` and
 * carries the `confirmation_ref` of the Ticket Sale it recorded, with no
 * redirect because there is nowhere to send anybody (ADR 0017).
 */
export type BeginCheckoutResult = {
  client_transaction_id: string;
  status: "pending" | "approved";
  /** Set only on a pending checkout — where the buyer goes to pay. */
  redirect_url?: string;
  /** Set only on an approved checkout — the buyer has their tickets already. */
  confirmation_ref?: string;
  amount_cents: number;
  currency: string;
};

/** The settled outcome of a Payment, as confirm reports it. */
export type ConfirmCheckoutResult = {
  client_transaction_id: string;
  status: "approved" | "failed";
  confirmation_ref?: string;
};

/**
 * beginCheckout starts an online checkout on a published Event. Guest by
 * definition: the form's own email, name, and Tax ID are all the API needs, and
 * a visitor with no session buys exactly like a signed-in one. Failures throw
 * APIError so the route handler can relay the API's own code and message.
 *
 * `sessionToken` is therefore optional and changes nothing about the sale. It
 * only tells the API that the buyer has proven they own the address they are
 * buying under, which is what lets a Tax ID typed here replace the one stored on
 * their profile instead of merely landing on this sale (ADR 0016).
 */
export async function beginCheckout(
  orgSlug: string,
  eventSlug: string,
  request: BeginCheckoutRequest,
  sessionToken?: string,
): Promise<BeginCheckoutResult> {
  const envelope = await callBackend<BeginCheckoutResult>(
    `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(eventSlug)}/checkout`,
    { method: "POST", body: JSON.stringify(request), sessionToken },
  );
  if (!envelope.data) {
    throw new APIError(
      502,
      { code: "INTERNAL_ERROR", message: "The checkout could not be started." },
      envelope.request_id ?? crypto.randomUUID(),
    );
  }
  return envelope.data;
}

/**
 * confirmCheckout settles the Payment named by our client transaction id,
 * relaying the provider's return-redirect params verbatim. Idempotent on the
 * API side: a refreshed return page gets the recorded outcome back.
 */
export async function confirmCheckout(
  clientTransactionId: string,
  providerParams: Record<string, string>,
): Promise<ConfirmCheckoutResult> {
  const envelope = await callBackend<ConfirmCheckoutResult>(
    `/api/v1/public/checkout/${encodeURIComponent(clientTransactionId)}/confirm`,
    { method: "POST", body: JSON.stringify({ provider_params: providerParams }) },
  );
  if (!envelope.data) {
    throw new APIError(
      502,
      { code: "INTERNAL_ERROR", message: "The payment could not be confirmed." },
      envelope.request_id ?? crypto.randomUUID(),
    );
  }
  return envelope.data;
}

/**
 * getCheckoutReversal asks whether the Ticket Sale one online checkout produced
 * can still be undone, and by when (#121, ADR 0018).
 *
 * It is how the checkout success page — a guest surface, reached by someone who
 * never had to sign in to buy — can state the Reversal Window at all. The key is
 * our own client transaction id, kept in the httpOnly checkout-context cookie
 * for the length of the round trip; no Customer Session exists here to scope the
 * read, and the response carries nothing about the buyer to protect.
 *
 * A read, and only a read: the reversal itself lives behind a Customer Session,
 * and this app offers the door to one rather than the action.
 *
 * fetchData's degradation is exactly right here. An API that is down leaves the
 * success page saying nothing about undoing, which is what it said before this
 * existed — a confirmation is still a confirmation.
 */
export async function getCheckoutReversal(
  clientTransactionId: string,
): Promise<ReversalOffer | null> {
  return fetchData<ReversalOffer>(
    `/api/v1/public/checkout/${encodeURIComponent(clientTransactionId)}/reversal`,
  );
}
