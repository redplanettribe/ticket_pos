import { relayTaxDocument } from "@/lib/tax-document-relay";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer's download of one Tax Document's signed XML (#475, ADR 0060);
 * the gating and the header handling are the relay's.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; documentId: string }> },
) {
  return relayTaxDocument(params, { suffix: "xml", defaultContentType: "application/xml" });
}
