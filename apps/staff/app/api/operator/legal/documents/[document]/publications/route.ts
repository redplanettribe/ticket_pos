import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// POST publishes the document's saved draft (#563): a new GATING EDITION, which
// re-gates everybody and takes effect no earlier than tomorrow, or a CORRECTION,
// which re-gates nobody, needs a typed reason and takes effect at once.
//
// A THIN RELAY, and deliberately so. Every rule this act has — the three review
// gates, the structural and locale-set refusals, the empty-diff asymmetry, the
// overnight delay, the protected language — is enforced by the API, because a
// precondition a browser could decline to check is not a precondition. This
// route adds the session and nothing else: it does NOT decide the label (the
// backend renders it from the lineage), does NOT carry the text (what is
// published is the saved draft), and does NOT name who published (that is taken
// from the Staff Session on the far side).
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/publications`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
