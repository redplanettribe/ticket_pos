import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type Membership = {
  member_id: string;
  organization_id: string;
  organization_name: string;
  organization_slug: string;
  organization_logo_url: string | null;
  role: string;
};

export async function GET() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return NextResponse.json(
      {
        data: null,
        error: { code: "UNAUTHORIZED", message: "Not signed in" },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    const envelope = await callBackend<Membership[]>("/api/v1/staff/memberships", {
      method: "GET",
      sessionToken: token,
    });
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
