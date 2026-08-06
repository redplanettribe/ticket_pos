import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken, type Follow } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * Follow and Unfollow a Tag (#218): the browser calls here, this handler
 * forwards under the Customer Session token held in this app's httpOnly cookie,
 * and hands back what the API said (ADR 0008 — no browser may address the API).
 *
 * The same relay as the Organization pair beside it, for the same reasons, and
 * deliberately no smarter: the token is the entire authorization, the canonical
 * key in the path names WHAT is being followed and never by whom, and every rule
 * lives on the far side — that following twice is idempotent, that a key naming
 * no Tag is a 404 rather than a Tag coined, that a Confirmation Link session is
 * too narrow to Follow at all. This handler decides none of them and relays the
 * API's own `error.code` and `error.message` so the control can say which
 * refusal it was in the reader's language (ADR 0023).
 *
 * Canonical keys are not URL-safe — several Preset Tags contain a space and one
 * an ampersand — so the segment arrives percent-encoded, is decoded by the
 * router into `params`, and is re-encoded on the way out.
 */
async function relay(canonicalKey: string, method: "POST" | "DELETE") {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  try {
    const backend = await callBackend<Follow | { message: string }>(
      `/api/v1/customer/follows/tags/${encodeURIComponent(canonicalKey)}`,
      { method, sessionToken: token },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}

export async function POST(
  _request: Request,
  { params }: { params: Promise<{ canonicalKey: string }> },
) {
  const { canonicalKey } = await params;
  return relay(canonicalKey, "POST");
}

export async function DELETE(
  _request: Request,
  { params }: { params: Promise<{ canonicalKey: string }> },
) {
  const { canonicalKey } = await params;
  return relay(canonicalKey, "DELETE");
}
