import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer's download of one Tax Document's RIDE (#497, ADR 0062), the PDF
 * beside the signed XML — the same bytes the operator downloads and the
 * delivery mail carries — streamed from the Go API through this relay with
 * its download headers kept, so the browser saves it under `<clave>.pdf`. The
 * shape of the XML relay beside it, with the PDF content type as the default.
 *
 * Both ids are relayed and nothing else is. The API gates the download on the
 * Sale exactly as it gates the XML: the session must own it, or be a
 * Confirmation Link session naming it, and every other case — another
 * Customer, another Sale, a document not yet authorized — is one 404 there.
 * That JSON envelope is passed through unchanged rather than turned into a
 * broken file.
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
    `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(documentId)}/ride`,
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
  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? "application/pdf");
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
