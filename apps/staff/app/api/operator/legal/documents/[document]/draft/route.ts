import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// PUT saves the document's one mutable draft, WHOLE (#561).
//
// The body is forwarded unread, as every proxy here forwards one. It carries the
// explicit published-language set and the artifact list in order — the order IS
// the fingerprint preimage's ordinal, and the API stamps it from the position,
// so no ordinal travels in this body and none can disagree with the list. Who
// saved it is taken from the session by the API and can never travel here.
//
// THIS PUBLISHES NOTHING. No page a reader can see changes, nobody is re-gated,
// and no fingerprint is computed. Publishing is #563.
export async function PUT(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/draft`,
      { method: "PUT", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// DELETE discards the draft, so the editor reopens on the current published
// edition — which is what makes an experiment something other than a commitment.
// Discarding a document with no draft is a success. No body.
export async function DELETE(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/draft`,
      { method: "DELETE", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
