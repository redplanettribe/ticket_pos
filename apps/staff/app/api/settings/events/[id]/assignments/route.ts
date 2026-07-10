import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type Assignment = {
  id: string;
  member_id: string;
  email: string;
  role: string;
  created_at: string;
};

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

export async function GET(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;

  try {
    const envelope = await callBackend<Assignment[]>(`/api/v1/staff/events/${id}/assignments`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
