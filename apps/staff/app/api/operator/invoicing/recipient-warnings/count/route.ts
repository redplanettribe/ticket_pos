import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorRecipientWarningCount } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns how many documents carry a Recipient Warning — the count the
// Operator Dashboard shows beside the needs_attention one, so a factura the
// SRI says went to the wrong taxpayer is never silent (#482, ADR 0061). Its
// own route, like the other counts. The Go API answers 404
// SALE_INVOICING_UNAVAILABLE while the feature is closed, forwarded as is.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorRecipientWarningCount>(
      "/api/v1/operator/invoicing/recipient-warnings/count",
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
