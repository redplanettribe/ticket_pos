import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorUninvoicedHouseSaleCount } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns how many Uninvoiced House Sales there are — the count the
// invoicing list shows beside the Recipient Warning one (#509, ADR 0064),
// so a backlog of facturas the platform owes is never silent. Its own
// route, like the other counts. The Go API answers 404
// SALE_INVOICING_UNAVAILABLE while the feature is closed, forwarded as is.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorUninvoicedHouseSaleCount>(
      "/api/v1/operator/invoicing/uninvoiced-sales/count",
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
