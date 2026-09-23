import { fetchBackendRaw } from "@/lib/api";
import { notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";
import { forwardDocument } from "@/lib/document-download";

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
 * through the BFF with its download headers kept (see forwardDocument), so the
 * browser saves it under the clave's filename and no shared cache keeps it.
 * The XML and the RIDE routes are this one relay with a different suffix and
 * default content type.
 *
 * Both ids are relayed and nothing else is. The API gates the download on the
 * Sale: the session must own it, or be a Confirmation Link session naming it,
 * and every other case - another Customer, another Sale, a document not yet
 * authorized - is one 404 there. That JSON envelope is passed through unchanged
 * rather than turned into a broken file.
 */
export async function relayTaxDocument(
  params: Promise<{ ticketSaleId: string; documentId: string }>,
  file: TaxDocumentFile,
): Promise<Response> {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId, documentId } = await params;
  const upstream = await fetchBackendRaw(
    `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(documentId)}/${file.suffix}`,
    { method: "GET", sessionToken: token },
  );

  return forwardDocument(upstream, file.defaultContentType);
}
