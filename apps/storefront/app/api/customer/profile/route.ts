import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";
import type { CustomerProfile } from "@/lib/profile";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The "My info" write: the browser posts the edited name, Tax ID and phone here, this
 * handler forwards it to the Go API under the Customer Session token held in
 * this app's httpOnly cookie, and hands back what the API said (ADR 0008 — no
 * browser may address the API).
 *
 * The token rides in Authorization exactly as it does on the checkout hop, but
 * with the opposite standing: there it is optional and merely tells the API who
 * is buying, while here it is the entire authorization for the write. Without
 * it there is nothing to edit, so a missing cookie is answered here rather than
 * by asking the API about a request it could only refuse.
 *
 * Validation is shape-only. The API owns the rules — non-blank names, the Tax ID
 * gradient, the refusal to serve a Confirmation Link session — and its error
 * envelope is relayed verbatim so the form can show the API's own message and
 * field details.
 */

type ProfileRequestBody = {
  first_name?: unknown;
  last_name?: unknown;
  tax_id_type?: unknown;
  tax_id_number?: unknown;
  phone?: unknown;
};

function asTrimmedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/**
 * Every clearable field on this endpoint — both Tax ID halves and the phone — is
 * either a non-empty string or null, and null is meaningful: it is how the
 * Customer withdraws something the platform holds about them. An absent or blank
 * value is therefore forwarded as null rather than as "", which the API would
 * have to guess at.
 *
 * The key is always forwarded, even as null. The API reads an ABSENT phone as
 * "this request is not about the phone, leave it alone" — a reading that exists
 * for clients written before the field, not for this one, which always knows
 * what the person in front of the form meant.
 */
function asClearable(value: unknown): string | null {
  const trimmed = asTrimmedString(value);
  return trimmed === "" ? null : trimmed;
}

export async function PATCH(request: Request) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  let body: ProfileRequestBody;
  try {
    body = (await request.json()) as ProfileRequestBody;
  } catch {
    return NextResponse.json(
      {
        data: null,
        error: { code: "VALIDATION_FAILED", message: "The change could not be read." },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  try {
    // The forwarded body names the five editable fields and nothing else. An
    // email in the request has nowhere to go: it is not read here, and the API
    // does not accept one either — the address is the Customer's identity.
    const envelope = await callBackend<CustomerProfile>("/api/v1/customer/profile", {
      method: "PATCH",
      body: JSON.stringify({
        first_name: asTrimmedString(body.first_name),
        last_name: asTrimmedString(body.last_name),
        tax_id_type: asClearable(body.tax_id_type),
        tax_id_number: asClearable(body.tax_id_number),
        phone: asClearable(body.phone),
      }),
      sessionToken: token,
    });
    return NextResponse.json({ data: envelope.data, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
