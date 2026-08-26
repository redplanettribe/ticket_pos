import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer's download of one Tax Document's signed XML (#475, ADR 0060),
 * streamed from the Go API through this relay with its download headers kept,
 * so the browser saves the file under the clave's filename — the shape of the
 * staff app's Sales Export proxy.
 *
 * Both ids are relayed and nothing else is. The API gates the download on the
 * Sale: the session must own it, or be a Confirmation Link session naming it,
 * and every other case — another Customer, another Sale, a document not yet
 * authorized — is one 404 there. That JSON envelope is passed through unchanged
 * rather than turned into a broken file.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; documentId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId, documentId } = await params;
  const upstream = await fetchBackendRaw(
    `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(documentId)}/xml`,
    { method: "GET", sessionToken: token },
  );

  if (!upstream.ok) {
    const body = await upstream.text();
    return new NextResponse(body, {
      status: upstream.status,
      headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
    });
  }

  const headers = new Headers();
  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? "application/xml");
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
