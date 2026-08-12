import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { consentEvidenceHeaders } from "@/lib/consent-evidence";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/** What one press of the confirmation link did, as the API reports it. */
type ConsentConfirmation = {
  marketing_consent: boolean;
  networking_consent: boolean;
  already_resolved: boolean;
  digest_enabled: boolean;
};

/**
 * Confirming a Pending Confirmation from a Sale Confirmation (#255, parent
 * #249, ADR 0035).
 *
 * THE SECOND ROUTE UNDER /api/customer THAT SENDS NO SESSION TOKEN, beside the
 * unsubscribe one and for a reason that is, if anything, stronger: a guest
 * checkout creates a Customer nobody has ever signed in as, so the owner of an
 * address a stranger typed may have no account to sign in to at all. The signed
 * token that came out of the email is the whole authority; what it can reach on
 * the far side is bounded to consents that are ALREADY pending on that address.
 *
 * IT IS A POST AND THERE IS NO GET HERE. Mail security scanners open every link
 * in every message before a human sees it. The link in the receipt points at the
 * /confirm-consent PAGE — which renders and acts on nothing — and only the
 * button on that page reaches this handler. A scanner that fetched either
 * address has confirmed nothing, which matters more here than it does for the
 * unsubscribe link: a prefetch that acted would GRANT a marketing opt-in nobody
 * ever gave, and leave evidence saying an inbox confirmed itself.
 *
 * The token travels in the BODY rather than in the query string, so it does not
 * end up in this app's access logs or in a Referer header on the way anywhere
 * else.
 *
 * IT FORWARDS THE THREE EVIDENCE HEADERS, exactly as the unsubscribe route
 * does: a press is a capture act and the API records it as a Consent Record with
 * the circumstances. It cannot observe them itself — no browser reaches it
 * directly (ADR 0008) — so a relay that dropped them would leave the record with
 * an empty technical proof.
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
        error: {
          code: "CONSENT_CONFIRMATION_LINK_INVALID",
          message: "This confirmation link is not valid.",
        },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    const backend = await callBackend<ConsentConfirmation>(
      "/api/v1/customer/consent/confirm",
      {
        method: "POST",
        body: JSON.stringify({ token }),
        headers: {
          ...consentEvidenceHeaders(request.headers),
        },
      },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
