import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * A Payout Request: an Organization's ask to be paid (ADR 0026). The snapshot of
 * where it asked to be paid rides along, because a later profile edit must not
 * change what an operator was told.
 */
type PayoutRequest = {
  id: string;
  amount_cents: number;
  note: string | null;
  status: string;
  requested_by: string;
  requested_at: string;
  payable_balance_cents: number;
  payout_profile: {
    bank_name: string;
    account_type: string;
    account_number: string;
    account_holder_name: string;
    tax_id_type: string;
    tax_id_number: string;
  };
  resolution_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
  /**
   * When an operator sent the transfer, and null until one is submitted. The
   * date the organizer's "up to 48 hours" sentence is built on (#187) — which
   * is why it is on this surface, while who submitted it and what the bank
   * called it stay on the operator's.
   */
  transfer_submitted_at: string | null;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

export async function GET() {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<PayoutRequest[]>("/api/v1/staff/organization/payout-requests", {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

export async function POST(request: Request) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const body = await request.json();
    const envelope = await callBackend<PayoutRequest>("/api/v1/staff/organization/payout-requests", {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    // The API answers 201 for a newly recorded ask and 200 when it hands back
    // the outstanding one instead (ADR 0024's courtesy, ADR 0026). Both are
    // successes carrying the same shape, and the page tells them apart by the
    // request's own id rather than by the status code — so forwarding the
    // envelope alone loses nothing.
    return NextResponse.json(envelope);
  } catch (error) {
    // jsonFromAPIError forwards the VALIDATION_FAILED details untouched, which
    // is what lets the form put each refusal beside the field it is about —
    // including the bank fields, since the request form IS the profile editor.
    return jsonFromAPIError(error);
  }
}
