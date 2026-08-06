import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse, notSignedInResponse } from "@/lib/bff";
import { customerSessionToken, type Follow } from "@/lib/customer-session";

// Reads the session cookie and writes through it; never cached.
export const dynamic = "force-dynamic";

/**
 * Follow and Unfollow an Organization: the browser calls here, this handler
 * forwards under the Customer Session token held in this app's httpOnly cookie,
 * and hands back what the API said (ADR 0008 — no browser may address the API).
 *
 * The token is the entire authorization for both writes. The slug in the path is
 * relayed and nothing else is: it names WHAT is being followed, never by whom —
 * the API takes the Customer from the session, so there is no field here that
 * could aim a Follow at somebody else's list.
 *
 * Every rule lives on the far side, including the two that are easy to
 * re-implement here by mistake: that following twice is idempotent rather than a
 * conflict, and that a Confirmation Link session is too narrow to Follow at all.
 * This handler decides neither and relays the API's own `error.code` and
 * `error.message` so the control can say which refusal it was in the language
 * the reader is reading (ADR 0023).
 */
async function relay(slug: string, method: "POST" | "DELETE") {
  const token = await customerSessionToken();
  if (!token) {
    return notSignedInResponse();
  }

  try {
    const backend = await callBackend<Follow | { message: string }>(
      `/api/v1/customer/follows/organizations/${encodeURIComponent(slug)}`,
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

export async function POST(_request: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return relay(slug, "POST");
}

export async function DELETE(_request: Request, { params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return relay(slug, "DELETE");
}
