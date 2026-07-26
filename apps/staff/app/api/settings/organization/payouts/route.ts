import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type PayoutsSummary = {
  withdrawable_balance_cents: number;
  currency: string;
  payouts: {
    id: string;
    amount_cents: number;
    paid_at: string;
    note: string | null;
  }[];
};

export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<PayoutsSummary>("/api/v1/staff/organization/payouts", {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
