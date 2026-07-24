import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { clientIpHeaders } from "@/lib/client-ip";

export async function POST(request: Request) {
  try {
    const body = await request.json();
    // The per-IP OTP limit counts whatever we send here, so this must be
    // derived (see lib/client-ip.ts) and never forwarded from the browser.
    const envelope = await callBackend<{ message: string }>("/api/v1/auth/otp/request", {
      method: "POST",
      body: JSON.stringify(body),
      headers: clientIpHeaders(request.headers),
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
