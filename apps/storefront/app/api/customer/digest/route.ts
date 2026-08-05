import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/** The Follow Digest switch as the API reports it back. */
type DigestSubscription = { digest_enabled: boolean };

/**
 * The Customer Area's Follow Digest toggle (#224, parent #215, ADR 0030).
 *
 * The same relay as the Follow routes beside it and deliberately no smarter: the
 * browser calls here, this handler forwards under the Customer Session token
 * held in this app's httpOnly cookie, and hands back what the API said (ADR 0008
 * — no browser may address the Go API).
 *
 * It carries no identifier of whose switch it is. The session is the only scope,
 * exactly as it is for the Follows listing, so nothing a page script can reach
 * names a Customer.
 *
 * A STATE AND NOT A FLIP travels in the body, which is the API's contract and is
 * worth not smoothing over here: "turn it off" arriving twice means what it
 * meant the first time, where a flip arriving twice would turn somebody's mail
 * back on after they asked for quiet.
 *
 * This is the only entry point that can turn the Digest back ON. The unsubscribe
 * link beside it is unauthenticated because a person who wants quiet must be
 * able to have it without signing in, and none of that argument applies to
 * subscribing an inbox to weekly mail.
 */
export async function PUT(request: Request) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  let enabled: unknown;
  try {
    ({ enabled } = (await request.json()) as { enabled?: unknown });
  } catch {
    enabled = undefined;
  }
  if (typeof enabled !== "boolean") {
    // Refused here rather than forwarded, because an absent field would reach
    // the API as nothing and the destructive half of this switch is a bool's
    // zero value. The API refuses it too; this saves a hop and says the same.
    // Shaped as the API shapes it: VALIDATION_FAILED carries details.fields[],
    // and the Storefront resolves the words from the code (ADR 0023). A hop
    // saved must still answer in the envelope the reader's error handling
    // expects, or the saving costs a rendered message.
    return NextResponse.json(
      {
        data: null,
        error: {
          code: "VALIDATION_FAILED",
          message: "Request validation failed",
          details: { fields: [{ field: "enabled", message: "must be true or false" }] },
        },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    const backend = await callBackend<DigestSubscription>("/api/v1/customer/digest", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
      sessionToken: token,
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
