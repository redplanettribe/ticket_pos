import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import type { AssignmentLinkView } from "@/lib/assignment-link";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Accepting a Ticket Assignment (#325, parent #322, ADR 0046).
 *
 * A BFF hop like every other: the browser calls this, this calls the Go API, and
 * what the API said comes back intact (ADR 0008).
 *
 * IT SENDS NO SESSION TOKEN AND READS NO COOKIE, like the answer-link and
 * consent routes beside it — and for a reason that goes further than theirs. The
 * reader has no account YET: this call is what creates one. Asking them to sign
 * in first would be asking somebody to prove an address in order to be allowed
 * to prove an address.
 *
 * IT IS A POST BECAUSE IT WRITES, and the page above it deliberately does not
 * call it on render. Mail security scanners open every link in every message
 * before a human sees it, and a page that accepted on being fetched would mint a
 * Verified Customer nobody proved and attach a stranger's name to an
 * Organization's guest list. The press is the act.
 *
 * THE TOKEN TRAVELS IN THE BODY. Not the path, not the query — so it reaches
 * neither this app's access log nor a Referer header on the way anywhere else.
 * It is the most sensitive token this platform hands out: what it does is assert
 * who somebody is.
 */
export async function POST(request: Request) {
  let body: Record<string, unknown>;
  try {
    body = (await request.json()) as Record<string, unknown>;
  } catch {
    return NextResponse.json(
      {
        data: null,
        error: { code: "INVALID_JSON", message: "We couldn't read that." },
        request_id: crypto.randomUUID(),
      },
      { status: 400 },
    );
  }

  const token = body.token;
  if (typeof token !== "string" || token.trim() === "") {
    return NextResponse.json(
      {
        data: null,
        error: { code: "ASSIGNMENT_LINK_INVALID", message: "This link is not valid." },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    const backend = await callBackend<AssignmentLinkView>("/api/v1/public/assignment-link", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
