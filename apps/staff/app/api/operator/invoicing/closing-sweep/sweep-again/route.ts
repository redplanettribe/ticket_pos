import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorClosingSweepStatus } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// Sweep again (#750, ADR 0078): makes the unswept facturas, and just those,
// due on the Drainer's next tick, and answers the dashboard's line as it
// then stands. No body.
export async function POST() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const envelope = await callBackend<OperatorClosingSweepStatus>(
      "/api/v1/operator/invoicing/closing-sweep/sweep-again",
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
