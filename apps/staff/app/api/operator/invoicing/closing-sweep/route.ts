import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorClosingSweepStatus } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET returns the Operator Dashboard's Closing Sweep line (#750, ADR 0078):
// the latest month whose sweep has ended and how many of its facturas
// couldn't be swept. Alive whatever Sale Invoicing's flag.
export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorClosingSweepStatus>("/api/v1/operator/invoicing/closing-sweep", {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
