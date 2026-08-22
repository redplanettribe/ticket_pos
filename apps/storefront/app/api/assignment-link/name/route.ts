import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import type { AssignmentLinkView } from "@/lib/assignment-link";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Giving the Holder's own name through an Assignment Link (#325, ADR 0046).
 *
 * PUT, because it states the whole current fact: one name per person, and
 * correcting a typo captured at some door sale years ago is the same request
 * with a different body.
 *
 * WHAT IT RELAYS IS TWO FIELDS AND A TOKEN, and it names them explicitly rather
 * than spreading the browser's body. That is not defensive tidiness: a Holder is
 * asked for their name and their Ticket Questions and NOTHING ELSE — never a Tax
 * ID, which is a fact about the sale's buyer — and a relay that forwarded
 * whatever arrived would be one route away from carrying one.
 *
 * No session, no cookie, and the token in the BODY, as on the accept above.
 */
export async function PUT(request: Request) {
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
    const backend = await callBackend<AssignmentLinkView>("/api/v1/public/assignment-link/name", {
      method: "PUT",
      // NAMED FIELDS, NEVER A SPREAD. See above: this body may carry a name and
      // a token and nothing else, whatever the browser sent.
      body: JSON.stringify({
        token,
        first_name: typeof body.first_name === "string" ? body.first_name : "",
        last_name: typeof body.last_name === "string" ? body.last_name : "",
      }),
    });
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
