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

/**
 * fetchBackendRaw proxies a request to the Go API and returns the raw Response
 * without decoding it. Use it for endpoints that stream binary bodies (the
 * .xlsx template) or accept multipart uploads (preview/commit), where the JSON
 * envelope helper does not apply. The caller forwards the result to the browser.
 */
export function fetchBackendRaw(
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
