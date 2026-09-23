import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, proxyJSONRead, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;

  return proxyJSONRead(request, `/api/v1/staff/events/${id}/affiliate-links`, token);
}

export async function POST(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/affiliate-links`, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope, { status: 201 });
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
