import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import type { AnswerLinkView } from "@/lib/answer-link";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Answering one Ticket Question through an Answer Link (#312, ADR 0044).
 *
 * A BFF hop like every other: the browser calls this, this calls the Go API, and
 * what the API said comes back intact (ADR 0008).
 *
 * IT SENDS NO SESSION TOKEN AND READS NO COOKIE — the third route in this app to
 * do so, beside the unsubscribe and consent-confirmation ones, and the reason is
 * the strongest of the three. Whoever holds an Answer Link is not the buyer, has
 * no account, and gave this platform no address; asking them to sign in would
 * mean collecting one, which is precisely what ADR 0044 refuses to do. The signed
 * token in the body is the whole authority, and what it reaches on the far side
 * is bounded to ONE Ticket's questions.
 *
 * IT IS OUTSIDE /api/customer FOR THAT REASON. A route under that prefix would
 * suggest a Customer is involved; none is, and none is created.
 *
 * THE TOKEN TRAVELS IN THE BODY. Not in the path and not in the query, so it
 * reaches neither this app's access log nor a Referer header on the way anywhere
 * else — and unlike a Confirmation Link's, this token has no expiry of its own to
 * limit how long a leaked copy stays useful.
 *
 * NO EVIDENCE HEADERS. The consent routes forward the caller's IP and user agent
 * because a press there is a capture act the platform records against a person.
 * Nothing here is recorded against anybody: migration 073 gives an Answer no
 * author column, deliberately, because recording one would mean recording an
 * identity for the very person this platform collects nothing about.
 */
export async function PUT(
  request: Request,
  { params }: { params: Promise<{ questionId: string }> },
) {
  const { questionId } = await params;

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
        error: { code: "ANSWER_LINK_INVALID", message: "This link is not valid." },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    // THE BODY IS RELAYED AS IT ARRIVED, and nothing about the Answer is
    // validated here. Every rule needs the Ticket Question's KIND, which this
    // hop does not have and must not guess: text is valid for two kinds and
    // refused by five, and a relay that decided which it was looking at would be
    // a second copy of catalog.ParseAnswer that could disagree with the first.
    // What comes back is a refusal carrying the kind and a problem token.
    const backend = await callBackend<AnswerLinkView>(
      `/api/v1/public/answer-link/questions/${encodeURIComponent(questionId)}`,
      { method: "PUT", body: JSON.stringify(body) },
    );
    return NextResponse.json(
      { data: backend.data, error: null, request_id: crypto.randomUUID() },
      { status: backend.status },
    );
  } catch (error) {
    return apiErrorResponse(error);
  }
}
