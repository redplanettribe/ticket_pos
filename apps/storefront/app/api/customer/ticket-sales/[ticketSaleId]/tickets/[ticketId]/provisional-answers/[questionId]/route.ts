import { relayCustomerPut } from "@/lib/bff";
import type { BuyerTicket } from "@/lib/buyer-answers";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The buyer correcting one Answer they gave for somebody else (#672, ADR
 * 0076): on an Event that requires Named Tickets, a Ticket assigned to
 * somebody who has not yet accepted keeps the buyer's Answers, and the buyer
 * may correct them until that person accepts.
 *
 * PUT because the far side is: it is the whole Answer every time, and a
 * correction is the same request with a different body.
 *
 * THE BODY IS RELAYED UNVALIDATED, as on the held-ticket answer route beside
 * it: every rule about what an Answer may be needs the question's KIND, which
 * this hop does not have and must not guess.
 *
 * NO TOKEN TRAVELS HERE. The Customer Session cookie is the whole credential;
 * the three ids in the path are relayed and nothing else is. The API resolves
 * the Ticket among the session's own Sale's provisional ones, so any other
 * Ticket is not found there rather than refused.
 *
 * THE WHOLE SALE COMES BACK, as after an assignment, and the page replaces its
 * list with it.
 */
export async function PUT(
  request: Request,
  { params }: { params: Promise<{ ticketSaleId: string; ticketId: string; questionId: string }> },
) {
  const { ticketSaleId, ticketId, questionId } = await params;
  return relayCustomerPut<BuyerTicket[]>(
    request,
    `/api/v1/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}` +
      `/tickets/${encodeURIComponent(ticketId)}` +
      `/provisional-answers/${encodeURIComponent(questionId)}`,
  );
}
