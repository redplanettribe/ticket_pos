import { digestOf } from "./caught-error.ts";
import { REQUEST_ID_HEADER } from "./reference-code.ts";

/**
 * Next's onRequestError arguments, restated rather than imported so this module
 * stays testable under the plain Node test runner (Next's types are the shape
 * of `Instrumentation.onRequestError`).
 */
type RequestErrorRequest = {
  path: string;
  method: string;
  headers: Record<string, string | string[] | undefined>;
};

type RequestErrorContext = {
  routerKind: "Pages Router" | "App Router";
  routePath: string;
  routeType: "render" | "route" | "action" | "middleware" | "proxy";
  renderSource?: "react-server-components" | "react-server-components-payload" | "server-rendering";
  revalidateReason?: string;
};

type ReportRequestError = (
  error: unknown,
  request: RequestErrorRequest,
  context: RequestErrorContext,
) => void | Promise<void>;

function headerValue(value: string | string[] | undefined): string | null {
  const first = Array.isArray(value) ? value[0] : value;
  return first ? first : null;
}

/**
 * Builds a Next app's onRequestError: an error Next caught in a server
 * render, route handler, server action or middleware.
 *
 * It writes one structured line to stdout, which Cloud Logging indexes, with the
 * request id the error screen shows as the reference code (forwarded by the
 * middleware, reference-code.ts), Next's digest for the error, and the route
 * pattern. So a reference code a person quotes finds the failure in Cloud
 * Logging whether or not Sentry is on, and the digest finds Next's own line with
 * the stack beside it. Never the error's text or the raw path: either can name a
 * person, and Next already prints the error.
 *
 * Then it reports the error; tagRequestId puts the id on the event. Except an
 * error from server-rendering a client component ("server-rendering"): the
 * browser renders that component again, and if it fails there too the error
 * screen reports it, so reporting it here as well would make one crash two
 * events. One that fails only on the server is in the log line. Next does not
 * call this for a not-found or a redirect.
 */
export function createOnRequestError(deps: {
  report: ReportRequestError;
  log: (line: string) => void;
}) {
  return async (error: unknown, request: RequestErrorRequest, context: RequestErrorContext) => {
    const requestId = headerValue(request.headers[REQUEST_ID_HEADER]);
    deps.log(
      JSON.stringify({
        severity: "ERROR",
        message: "request failed",
        request_id: requestId,
        digest: digestOf(error),
        route: context.routePath,
        route_type: context.routeType,
        render_source: context.renderSource ?? null,
      }),
    );
    if (context.renderSource === "server-rendering") return;
    await deps.report(error, request, context);
  };
}

/**
 * The part of a Sentry event tagRequestId reads and writes, restated so this
 * module needs no SDK types.
 */
type ServerEvent = {
  tags?: Record<string, unknown>;
  sdkProcessingMetadata?: { normalizedRequest?: { headers?: Record<string, string | undefined> } };
};

/**
 * Tags a server event with the id of the request it happened in, read from the
 * request headers the SDK holds for it (the middleware forwarded the id there).
 *
 * An event processor rather than a tag set where the error is reported, because
 * a crash in a page or layout is captured first by the SDK's own server
 * component wrapper, and onRequestError's capture of the same error is then
 * dropped as a duplicate: only an event processor reaches whichever capture
 * wins. A tag already set is kept.
 */
export function tagRequestId<T extends ServerEvent>(event: T): T {
  const requestId = event.sdkProcessingMetadata?.normalizedRequest?.headers?.[REQUEST_ID_HEADER];
  if (requestId && event.tags?.request_id === undefined) {
    event.tags = { ...event.tags, request_id: requestId };
  }
  return event;
}
