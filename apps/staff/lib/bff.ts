import { NextResponse } from "next/server";

export function jsonFromAPIError(error: unknown) {
  if (error && typeof error === "object" && "status" in error && "code" in error) {
    const apiError = error as {
      status: number;
      code: string;
      message: string;
      requestId: string;
      details?: unknown;
    };
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

export function unauthorizedResponse() {
  return NextResponse.json(
    {
      data: null,
      error: { code: "UNAUTHORIZED", message: "Not signed in" },
      request_id: crypto.randomUUID(),
    },
    { status: 401 },
  );
}
