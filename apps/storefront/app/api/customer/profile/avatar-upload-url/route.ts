import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";

// Reads the session cookie; never cached.
export const dynamic = "force-dynamic";

/**
 * Mints a presigned upload URL for the Customer's Avatar (ADR 0008 — the
 * browser talks to this app, never to the Go API). The upload itself then goes
 * straight from the browser to object storage; attaching the uploaded image is
 * a separate PUT to /api/customer/profile/avatar with the object key.
 *
 * Validation is shape-only: the API owns the content-type allowlist and the
 * full-session requirement, and its envelope is relayed verbatim.
 */

type UploadURLRequestBody = {
  content_type?: unknown;
  file_name?: unknown;
};

export type AvatarUploadTicket = {
  upload_url: string;
  object_key: string;
  public_url: string;
};

export async function POST(request: Request) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  let body: UploadURLRequestBody;
  try {
    body = (await request.json()) as UploadURLRequestBody;
  } catch {
    body = {};
  }

  try {
    const envelope = await callBackend<AvatarUploadTicket>(
      "/api/v1/customer/profile/avatar-upload-url",
      {
        method: "POST",
        body: JSON.stringify({
          content_type: typeof body.content_type === "string" ? body.content_type : "",
          file_name: typeof body.file_name === "string" ? body.file_name : null,
        }),
        sessionToken: token,
      },
    );
    return NextResponse.json({ data: envelope.data, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
