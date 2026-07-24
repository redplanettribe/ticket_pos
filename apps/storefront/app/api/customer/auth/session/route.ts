import { sessionOutcomeResponse } from "@/lib/bff";
import { getCustomerSession } from "@/lib/customer-session";

/**
 * Returns which email the visitor is signed in as, for browser code that needs
 * to know. Reading the session also extends its sliding window on the API.
 *
 * A session that has expired or been signed out is reported as "not signed in"
 * rather than as an error: the caller's recovery is to offer sign-in. That
 * mapping, and the rest of the relay, lives in sessionOutcomeResponse.
 */
export async function GET() {
  return sessionOutcomeResponse(await getCustomerSession());
}
