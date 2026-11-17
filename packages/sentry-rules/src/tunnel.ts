/**
 * What the same-origin tunnel route may forward (ADR 0079).
 *
 * Browser events are posted to the app's own origin, so ad blockers do not drop
 * them and Sentry never sees a buyer's IP address. A route that forwards
 * whatever it is handed would be an open relay into anyone's Sentry project, so
 * it forwards an envelope only when the envelope is addressed to the app's own
 * Sentry host and project, and always to the endpoint built from the app's own
 * DSN, never from anything in the request.
 *
 * An envelope is newline-separated: a JSON header line, then items, each a
 * header line and a payload that may be binary. The header carries the DSN the
 * SDK built it for when it is sent through a tunnel
 * (https://develop.sentry.dev/sdk/data-model/envelopes/). Only that first line
 * is read.
 *
 * A DSN is `{protocol}://{public_key}@{host}[:{port}]{path}/{project_id}`, and
 * its envelope endpoint is `{protocol}://{host}[:{port}]{path}/api/{project_id}/envelope/`.
 */

export type TunnelDecision = { forward: true; url: string } | { forward: false };

interface Dsn {
  protocol: string;
  host: string;
  path: string;
  projectId: string;
}

/**
 * Whether to forward an envelope, and where. With no DSN, an unreadable one, or
 * an envelope for any other host, path or project, nothing is forwarded.
 */
export function decideTunnel(envelope: string | Uint8Array, ownDsn: string | undefined): TunnelDecision {
  const own = parseDsn(ownDsn);
  if (own === null) return { forward: false };

  const addressed = parseDsn(envelopeDsn(envelope));
  if (addressed === null) return { forward: false };

  if (addressed.host !== own.host || addressed.path !== own.path || addressed.projectId !== own.projectId) {
    return { forward: false };
  }
  return { forward: true, url: `${own.protocol}//${own.host}${own.path}/api/${own.projectId}/envelope/` };
}

function envelopeDsn(envelope: string | Uint8Array): string | undefined {
  let headerLine: string;
  if (typeof envelope === "string") {
    const end = envelope.indexOf("\n");
    headerLine = end === -1 ? envelope : envelope.slice(0, end);
  } else {
    const end = envelope.indexOf(0x0a);
    headerLine = new TextDecoder().decode(end === -1 ? envelope : envelope.subarray(0, end));
  }
  let header: unknown;
  try {
    header = JSON.parse(headerLine);
  } catch {
    return undefined;
  }
  if (header === null || typeof header !== "object" || Array.isArray(header)) return undefined;
  const dsn = (header as { dsn?: unknown }).dsn;
  return typeof dsn === "string" ? dsn : undefined;
}

function parseDsn(dsn: string | undefined): Dsn | null {
  if (dsn === undefined || dsn.trim() === "") return null;
  let url: URL;
  try {
    url = new URL(dsn.trim());
  } catch {
    return null;
  }
  if ((url.protocol !== "https:" && url.protocol !== "http:") || url.username === "") return null;
  const segments = url.pathname.split("/");
  const projectId = segments.pop() ?? "";
  if (!/^[0-9]+$/.test(projectId)) return null;
  return { protocol: url.protocol, host: url.host, path: segments.join("/"), projectId };
}
