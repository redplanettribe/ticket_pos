import { NextResponse } from "next/server";

import { callBackend } from "./api";
import { proxyRead } from "./reader-abort";

/**
 * proxyJSONRead passes a GET straight through to the API with the reader's
 * session and abort signal, and hands the envelope back as it came. A reader
 * who leaves stops the API's work and is answered quietly (see proxyRead); any
 * other failure is the API's envelope, as jsonFromAPIError maps it.
 */
export function proxyJSONRead(request: Request, path: string, sessionToken: string): Promise<Response> {
  return proxyRead(
    request,
    async (signal) => NextResponse.json(await callBackend<unknown>(path, { method: "GET", sessionToken, signal })),
    jsonFromAPIError,
  );
}

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
