type APIEnvelope<T> = {
  data: T | null;
  error: {
    code: string;
    message: string;
    details?: unknown;
  } | null;
  request_id: string;
};

export class APIError extends Error {
  code: string;
  details?: unknown;
  requestId: string;
  status: number;

  constructor(status: number, envelope: NonNullable<APIEnvelope<unknown>["error"]>, requestId: string) {
    super(envelope.message);
    this.code = envelope.code;
    this.details = envelope.details;
    this.requestId = requestId;
    this.status = status;
  }
}

function apiBaseUrl(): string {
  const url = process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return url.replace(/\/$/, "");
}

// --- Service-to-service authentication (ADR 0008) -------------------------
//
// The Go API is deployed with unauthenticated invocation disabled, so every
// server-side call from this app must carry a Google-signed OIDC ID token
// naming the API service URL as its audience. The token is fetched from the
// Cloud Run metadata server; it is a credential for *this service*, distinct
// from the end user's session token.
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
 * applyServiceAuth attaches the service credential to an outgoing API request.
 *
 * It goes in X-Serverless-Authorization, not Authorization: Cloud Run checks
 * that header for IAM when it is present and strips it before the container
 * sees the request, which leaves Authorization free to carry the end user's
 * session token exactly as it does locally. Using Authorization for the ID
 * token would displace the session and break every authenticated endpoint.
 */
async function applyServiceAuth(headers: Headers): Promise<void> {
  const token = await serviceIdToken();
  if (token) {
    headers.set("X-Serverless-Authorization", `Bearer ${token}`);
  }
}

/**
 * fetchBackendRaw proxies a request to the Go API and returns the raw Response
 * without decoding it. Use it for endpoints that stream binary bodies (the
 * .xlsx template) or accept multipart uploads (preview/commit), where the JSON
 * envelope helper does not apply. The caller forwards the result to the browser.
 */
export async function fetchBackendRaw(
  path: string,
  init: RequestInit & { sessionToken?: string } = {},
): Promise<Response> {
  const headers = new Headers(init.headers);
  if (init.sessionToken) {
    headers.set("Authorization", `Bearer ${init.sessionToken}`);
  }
  if (!headers.has("X-Request-ID")) {
    headers.set("X-Request-ID", crypto.randomUUID());
  }
  await applyServiceAuth(headers);
  return fetch(`${apiBaseUrl()}${path}`, { ...init, headers });
}

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
  await applyServiceAuth(headers);

  const response = await fetch(`${apiBaseUrl()}${path}`, {
    ...init,
    headers,
  });

  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new APIError(
      response.status,
      envelope.error ?? {
        code: "INTERNAL_ERROR",
        message: "Request failed",
      },
      envelope.request_id,
    );
  }
  return envelope;
}
