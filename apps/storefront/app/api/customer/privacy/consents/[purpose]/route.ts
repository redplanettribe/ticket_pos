import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { consentEvidenceHeaders } from "@/lib/consent-evidence";
import { customerSessionToken } from "@/lib/customer-session";
import type { OptionalConsents } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * One control on the Privacy page (#268, parent #265): grant or withdraw one
 * optional consent.
 *
 * THE SAME THIN RELAY AS THE DIGEST TOGGLE BESIDE IT AND DELIBERATELY NO
 * SMARTER. The browser calls here, this handler forwards under the Customer
 * Session token held in this app's httpOnly cookie, and hands back what the API
 * said (ADR 0008 — no browser may address the Go API). THE API OWNS EVERY RULE:
 * which purposes exist, what a withdrawal is, whether one took anything away,
 * whether anybody is mailed, and that Marketing Consent and the Follow Digest
 * move together. Nothing about any of that is decided here, and a rule that
 * appeared here would be a second copy of one the API already enforces — the
 * copy that gets it wrong is always the one a browser can skip.
 *
 * The purpose travels in the PATH and is forwarded verbatim rather than checked
 * against a list this file holds. The API's vocabulary is closed and it refuses
 * what is outside it with VALIDATION_FAILED, which the Storefront already words
 * from the code (ADR 0023); a copy of the list here would only have to be kept
 * in step with one that is already authoritative. It is encoded before it is
 * concatenated, so nothing a caller types can reach the API as another path.
 *
 * IT FORWARDS THE THREE EVIDENCE HEADERS, exactly as the consent submission and
 * the digest toggle do. Moving a control is a consent act and the API records
 * the circumstances of it; the API can observe none of them for itself, so a
 * relay that dropped them would leave a Consent Record — the evidence of
 * somebody exercising a legal right — with an empty technical proof. The IP is
 * derived from the forwarding chain and never from the browser's own copy of it.
 *
 * It carries no identifier of whose consent it is. The session is the only
 * scope, so nothing a page script can reach names a Customer.
 */
export async function PUT(request: Request, { params }: { params: Promise<{ purpose: string }> }) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  const { purpose } = await params;

  let granted: unknown;
  try {
    ({ granted } = (await request.json()) as { granted?: unknown });
  } catch {
    granted = undefined;
  }
  if (typeof granted !== "boolean") {
    // Refused here rather than forwarded, because an absent field would reach
    // the API as nothing and a bool's zero value is the destructive half of this
    // control. The API refuses it too; this saves a hop and says the same, in
    // the envelope the reader's error handling expects (ADR 0023).
    return NextResponse.json(
      {
        data: null,
        error: {
          code: "VALIDATION_FAILED",
          message: "Request validation failed",
          details: { fields: [{ field: "granted", message: "must be true or false" }] },
        },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    const backend = await callBackend<OptionalConsents>(
      `/api/v1/customer/privacy/consents/${encodeURIComponent(purpose)}`,
      {
        method: "PUT",
        body: JSON.stringify({ granted }),
        sessionToken: token,
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
