import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string; ticketTypeId: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// Setting and updating a Promotion both state the whole thing — price and
// window — so the two verbs differ only in which slot state they expect.
async function writePromotion(request: Request, context: RouteContext, method: "POST" | "PATCH") {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketTypeId } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-types/${ticketTypeId}/promotion`,
      {
        method,
        sessionToken: token,
        body: JSON.stringify(body),
      },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

export async function POST(request: Request, context: RouteContext) {
  return writePromotion(request, context, "POST");
}

export async function PATCH(request: Request, context: RouteContext) {
  return writePromotion(request, context, "PATCH");
}

export async function DELETE(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id, ticketTypeId } = await context.params;

  try {
    const envelope = await callBackend<unknown>(
      `/api/v1/staff/events/${id}/ticket-types/${ticketTypeId}/promotion`,
      {
        method: "DELETE",
        sessionToken: token,
      },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
