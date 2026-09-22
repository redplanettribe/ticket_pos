import { fetchBackendRaw } from "@/lib/api";
import { forwardDownload } from "@/lib/download-proxy";

/**
 * Forwards a Tax Invoice document download from the Go API as-is (#456): an
 * error is the API's JSON envelope passed through unchanged so the caller can
 * show the reason; a success keeps the content type and the filename the API
 * chose, so the browser saves the file under the clave. The same pass-through
 * as the Sales Export proxy, and the same code (lib/download-proxy).
 */
export async function proxyInvoiceDocument(path: string, token: string): Promise<Response> {
  const upstream = await fetchBackendRaw(path, { method: "GET", sessionToken: token });
  return forwardDownload(upstream, "application/xml; charset=utf-8");
}
