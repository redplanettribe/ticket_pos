import { relayTaxDocument } from "@/lib/tax-document-relay";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer's download of one Tax Document's RIDE (#497, ADR 0062), the PDF
 * beside the signed XML — the same bytes the operator downloads and the
 * delivery mail carries; the gating and the header handling are the relay's.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; documentId: string }> },
) {
  return relayTaxDocument(params, { suffix: "ride", defaultContentType: "application/pdf" });
}
