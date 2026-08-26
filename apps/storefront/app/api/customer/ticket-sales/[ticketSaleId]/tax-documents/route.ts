import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";
import type { SaleDocument } from "@/lib/sale-documents";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * The Tax Documents of one of the buyer's Ticket Sales (#475, ADR 0060): the
 * factura a paid House sale owes, in the buyer's words — `authorized` with a
 * download, or `on_its_way` — and nothing the SRI ever said.
 *
 * The browser asks here, this handler forwards under the Customer Session token
 * held in this app's httpOnly cookie, and hands back what the API said — no
 * browser may address the Go API directly (ADR 0008).
 *
 * The id in the path is relayed and nothing else is. It names WHICH of the
 * caller's own sales, and the API scopes it to the Customer on the session —
 * and narrows it again to the single sale a Confirmation Link session names —
 * so an id belonging to somebody else resolves to an empty list there, exactly
 * as a sale that owes no document does.
 */
export async function GET(
  _request: Request,
  { params }: { params: Promise<{ ticketSaleId: string }> },
) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { ticketSaleId } = await params;

  try {
    const backend = await callBackend<SaleDocument[]>(
      `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents`,
      { sessionToken: token },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
