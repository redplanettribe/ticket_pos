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
 * inventing copy (docs/design/README.md: show API messages faithfully).
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
): Promise<APIEnvelope<T>> {
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
  return envelope;
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
  tags: { name: string; curated: boolean }[];
};

export type PublicTicketType = {
  name: string;
  description: string | null;
  price_cents: number;
  currency: string;
  remaining: number;
  sold_out: boolean;
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
  has_ended: boolean;
  organization: PublicOrganizationSummary;
  currency: string;
  ticket_types: PublicTicketType[];
  tags: { name: string; curated: boolean }[];
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

async function fetchData<T>(path: string): Promise<T | null> {
  // Deliberately outside the try: a missing service credential is a
  // deployment fault and must surface, while an API that is merely down or
  // unhappy still degrades to an empty page.
  const headers = await serviceAuthHeaders();
  try {
    const response = await fetch(`${apiBaseUrl()}${path}`, { cache: "no-store", headers });
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

export async function listPublicEvents(params: ListEventsParams = {}): Promise<PublicEventPage | null> {
  const query = new URLSearchParams();
  if (params.q) query.set("q", params.q);
  if (params.from) query.set("from", params.from);
  if (params.to) query.set("to", params.to);
  if (params.tags && params.tags.length > 0) query.set("tags", params.tags.join(","));
  if (params.cursor) query.set("cursor", params.cursor);
  if (params.limit) query.set("limit", String(params.limit));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return fetchData<PublicEventPage>(`/api/v1/public/events${suffix}`);
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

export async function getPublicEvent(
  orgSlug: string,
  eventSlug: string,
): Promise<PublicEventDetail | null> {
  return fetchData<PublicEventDetail>(
    `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(eventSlug)}`,
  );
}
