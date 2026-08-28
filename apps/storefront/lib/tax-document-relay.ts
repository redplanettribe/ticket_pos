import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";

/**
 * Which of a Tax Document's two files is relayed: the signed XML (#475, ADR
 * 0060) or the RIDE, the PDF beside it (#497, ADR 0062). The suffix is the
 * API's path segment; the default content type stands in only when the API
 * sends none.
 */
export type TaxDocumentFile = {
  suffix: "xml" | "ride";
  defaultContentType: "application/xml" | "application/pdf";
};

/**
 * The buyer's download of one Tax Document's file, streamed from the Go API
 * through the BFF with its download headers kept, so the browser saves it
 * under the clave's filename — the shape of the staff app's Sales Export
 * proxy and its `proxyInvoiceDocument`. The XML and the RIDE routes are this
 * one relay with a different suffix and default content type.
 *
 * Both ids are relayed and nothing else is. The API gates the download on the
 * Sale: the session must own it, or be a Confirmation Link session naming it,
 * and every other case — another Customer, another Sale, a document not yet
 * authorized — is one 404 there. That JSON envelope is passed through unchanged
 * rather than turned into a broken file.
 */
export async function relayTaxDocument(
  params: Promise<{ ticketSaleId: string; documentId: string }>,
  file: TaxDocumentFile,
): Promise<NextResponse> {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId, documentId } = await params;
  const upstream = await fetchBackendRaw(
    `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(documentId)}/${file.suffix}`,
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
  headers.set("Content-Type", upstream.headers.get("Content-Type") ?? file.defaultContentType);
  const disposition = upstream.headers.get("Content-Disposition");
  if (disposition) {
    headers.set("Content-Disposition", disposition);
  }
  return new NextResponse(upstream.body, { status: 200, headers });
}
