import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/** The Follow Digest switch as the API reports it back. */
type DigestSubscription = { digest_enabled: boolean };

/**
 * Unsubscribing from the Follow Digest (#224, parent #215, ADR 0030).
 *
 * THE ONLY ROUTE UNDER /api/customer THAT SENDS NO SESSION TOKEN, and that is
 * the feature rather than an oversight. A Digest is read in a mail client months
 * after anybody last signed in, so an opt-out gated behind a passcode would be
 * no opt-out at all. The signed token that came out of the email is the whole
 * authority; it names one Customer, and the only thing it can do on the far side
 * is set one reversible flag.
 *
 * IT IS A POST AND THERE IS NO GET HERE, which is the acceptance criterion the
 * whole design turns on. Mail security scanners open every link in every message
 * before a human sees it. The link in the Digest points at the /unsubscribe PAGE
 * — which renders and acts on nothing — and only the button on that page reaches
 * this handler. A scanner that fetched either address has changed nothing.
 *
 * The token travels in the BODY rather than in the query string, so it does not
 * end up in this app's access logs or in a Referer header on the way to
 * anywhere else.
 */
export async function POST(request: Request) {
  let token: unknown;
  try {
    ({ token } = (await request.json()) as { token?: unknown });
  } catch {
    token = undefined;
  }
  if (typeof token !== "string" || token.trim() === "") {
    return NextResponse.json(
      {
        data: null,
        error: { code: "UNSUBSCRIBE_LINK_INVALID", message: "This unsubscribe link is not valid." },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    const backend = await callBackend<DigestSubscription>("/api/v1/customer/unsubscribe", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
