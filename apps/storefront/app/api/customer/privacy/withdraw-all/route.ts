import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { consentEvidenceHeaders } from "@/lib/consent-evidence";
import { customerSessionToken } from "@/lib/customer-session";
import type { WithdrawAll } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * Withdraw All on the Privacy page (#269, parent #265): every optional consent
 * taken back in one act.
 *
 * THE SAME THIN RELAY AS THE PER-PURPOSE CONTROL BESIDE IT AND DELIBERATELY NO
 * SMARTER. The browser calls here, this handler forwards under the Customer
 * Session token held in this app's httpOnly cookie, and hands back what the API
 * said (ADR 0008 — no browser may address the Go API). THE API OWNS EVERY RULE:
 * what the act writes, that it is ONE Consent Record and not two, whether it
 * took anything away, whether anybody is mailed, and that the Follow Digest goes
 * off with Marketing Consent.
 *
 * IT IS A ROUTE OF ITS OWN, MIRRORING THE API'S. It would have been shorter to
 * have the browser call the per-purpose route twice, and that is exactly what
 * must not happen: two requests would leave the same Customer in the same state
 * while writing evidence that says they moved two controls, where what happened
 * was one person asking to be left alone. One act, one request, one row.
 *
 * IT SENDS NO BODY, because the act names nothing. There is no field here in
 * which a caller could ask for a grant, select one consent, or say anything at
 * all — which is why this file, unlike the control beside it, validates nothing.
 *
 * It forwards the three evidence headers, as every consent act does: the API can
 * observe none of them for itself, and a relay that dropped them would leave the
 * evidence of somebody exercising a legal right with an empty technical proof.
 */
export async function POST(request: Request) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  try {
    const backend = await callBackend<WithdrawAll>("/api/v1/customer/privacy/withdraw-all", {
      method: "POST",
      sessionToken: token,
      headers: {
        ...consentEvidenceHeaders(request.headers),
      },
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
