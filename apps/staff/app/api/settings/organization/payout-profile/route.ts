import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/** Where the Organization is paid (ADR 0025). Null when it has never said. */
type PayoutProfile = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
  updated_at: string;
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
    const envelope = await callBackend<PayoutProfile | null>("/api/v1/staff/organization/payout-profile", {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

export async function PUT(request: Request) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const body = await request.json();
    const envelope = await callBackend<PayoutProfile>("/api/v1/staff/organization/payout-profile", {
      method: "PUT",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    // jsonFromAPIError forwards the VALIDATION_FAILED details untouched, which
    // is what lets the form put each refusal beside the field it is about.
    return jsonFromAPIError(error);
  }
}
