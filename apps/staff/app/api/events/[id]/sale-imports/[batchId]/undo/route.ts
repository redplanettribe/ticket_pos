import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; batchId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// POST forwards the undo request (with the notify_buyers flag) for the latest
// Sale Import batch to the Go undo endpoint and returns its JSON envelope. The
// browser talks only to the BFF; the session is attached here.
export async function POST(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, batchId } = await context.params;

  let notifyBuyers = false;
  try {
    const body = (await request.json()) as { notify_buyers?: boolean };
    notifyBuyers = body.notify_buyers === true;
  } catch {
    // No/invalid body: default to not notifying.
  }

  try {
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/sale-imports/${batchId}/undo`, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify({ notify_buyers: notifyBuyers }),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
