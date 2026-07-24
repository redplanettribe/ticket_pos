import { sessionOutcomeResponse } from "@/lib/bff";
import { getCustomerArea } from "@/lib/customer-session";

/**
 * Reads the Customer Area: the signed-in Customer's Ticket Sales, upcoming and
 * past, across every Organization they have bought from.
 *
 * This route accepts no query, path, or body parameters, and forwards none. The
 * only thing it sends the API is the session token from the httpOnly cookie, and
 * the API scopes the read to the Customer on that session — so no identifier a
 * browser could supply has anywhere to go.
 */
export async function GET() {
  return sessionOutcomeResponse(await getCustomerArea());
}
