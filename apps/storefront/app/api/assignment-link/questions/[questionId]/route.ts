import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import type { AssignmentLinkView } from "@/lib/assignment-link";

// Acts on a request; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Answering one Ticket Question as the Holder who accepted the ticket (#325,
 * ADR 0044, ADR 0046).
 *
 * THE TOKEN IS THE ONLY CREDENTIAL, and it is one the platform mailed to the
 * person themselves, whose press proved the address. There is no other door
 * onto a Ticket's questions for anybody but Event Staff — the forwarded Answer
 * Link is retired (ADR 0049) — so an answer given here is the Holder's own and
 * nobody holding a copied link can overwrite it.
 *
 * THE BODY IS RELAYED AS IT ARRIVED, and nothing about the Answer is validated
 * here: every rule needs the Ticket Question's KIND, which this hop does not
 * have and must not guess. A relay that decided which field it was looking at
 * would be a second copy of catalog.ParseAnswer that could disagree with the
 * first.
 *
 * NO EVIDENCE HEADERS, as on the Answer Link's route: an Answer has no author
 * column (migration 073), deliberately.
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
        error: { code: "ASSIGNMENT_LINK_INVALID", message: "This link is not valid." },
        request_id: crypto.randomUUID(),
      },
      { status: 401 },
    );
  }

  try {
    const backend = await callBackend<AssignmentLinkView>(
      `/api/v1/public/assignment-link/questions/${encodeURIComponent(questionId)}`,
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
