import { sessionOutcomeResponse } from "@/lib/bff";
import { getFollows } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * Reads what the signed-in Customer Follows: one list covering every kind of
 * Follow, each entry carrying its own `type`.
 *
 * Like the Customer Area hop beside it, this route accepts no query, path, or
 * body parameters and forwards none. The only thing it sends the API is the
 * session token from the httpOnly cookie, and the API scopes the read to the
 * Customer on that session — so no identifier a browser could supply has
 * anywhere to go.
 */
export async function GET() {
  return sessionOutcomeResponse(await getFollows());
}
