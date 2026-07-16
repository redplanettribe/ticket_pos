import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

export async function POST(request: Request) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>("/api/v1/staff/organization/logo-upload-url", {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
