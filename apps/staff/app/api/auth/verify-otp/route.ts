import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME, sessionCookieOptions } from "@/lib/session";

type VerifyData = {
  session_id: string;
  session: {
    email: string;
    memberships: Array<{ member_id: string }>;
    active_member: { member_id: string } | null;
  };
};

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<VerifyData>("/api/v1/auth/otp/verify", {
      method: "POST",
      body: JSON.stringify(body),
    });

    const cookieStore = await cookies();
    cookieStore.set(SESSION_COOKIE_NAME, envelope.data!.session_id, sessionCookieOptions());

    return NextResponse.json(envelope);
  } catch (error) {
    return handleAPIError(error);
  }
}

function handleAPIError(error: unknown) {
  if (error && typeof error === "object" && "status" in error && "code" in error) {
    const apiError = error as { status: number; code: string; message: string; requestId: string; details?: unknown };
    return NextResponse.json(
      {
        data: null,
        error: {
          code: apiError.code,
          message: apiError.message,
          details: apiError.details,
        },
        request_id: apiError.requestId,
      },
      { status: apiError.status },
    );
  }
  return NextResponse.json(
    {
      data: null,
      error: { code: "INTERNAL_ERROR", message: "Unexpected error" },
      request_id: crypto.randomUUID(),
    },
    { status: 500 },
  );
}
