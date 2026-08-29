import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorBackfillResult } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// POST performs a Sale Invoice Backfill (#508, ADR 0064): the body's
// `ticket_sale_ids` are forwarded verbatim, and the Go API answers 200 with
// the owed and refused sales whenever the request itself is valid — a
// refusal is data on the result, never an HTTP error. 400 VALIDATION_FAILED
// on an empty, oversized or malformed list, 403 off the allowlist, and 404
// SALE_INVOICING_UNAVAILABLE while the feature is closed, all forwarded as is.
export async function POST(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorBackfillResult>(
      "/api/v1/operator/invoicing/uninvoiced-sales/backfill",
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
