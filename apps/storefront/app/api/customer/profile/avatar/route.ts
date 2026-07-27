import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken } from "@/lib/customer-session";
import type { CustomerProfile } from "@/lib/profile";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * The Avatar writes: PUT attaches an uploaded image by its object key, DELETE
 * removes the current one (reverting the Customer to initials). Both forward to
 * the Go API under the session token in this app's httpOnly cookie (ADR 0008)
 * and hand back the profile as it now stands.
 *
 * The key is opaque here. The API refuses any key outside the signed-in
 * Customer's own prefix, so nothing this handler could forward can point an
 * Avatar at someone else's image.
 */

type AvatarRequestBody = {
  image_key?: unknown;
};

export async function PUT(request: Request) {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  let body: AvatarRequestBody;
  try {
    body = (await request.json()) as AvatarRequestBody;
  } catch {
    body = {};
  }

  try {
    const envelope = await callBackend<CustomerProfile>("/api/v1/customer/profile/avatar", {
      method: "PUT",
      body: JSON.stringify({
        image_key: typeof body.image_key === "string" ? body.image_key.trim() : "",
      }),
      sessionToken: token,
    });
    return NextResponse.json({ data: envelope.data, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}

export async function DELETE() {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  try {
    const envelope = await callBackend<CustomerProfile>("/api/v1/customer/profile/avatar", {
      method: "DELETE",
      sessionToken: token,
    });
    return NextResponse.json({ data: envelope.data, error: null, request_id: crypto.randomUUID() });
  } catch (error) {
    return apiErrorResponse(error);
  }
}
