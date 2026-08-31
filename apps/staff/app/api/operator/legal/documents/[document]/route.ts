import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// GET returns one legal document's workspace: the published edition, the one
// mutable draft, and the languages a draft may publish in (#561, spec #556).
//
// One read and not three, so the editor cannot diff a draft against text from a
// different edition. `document` is `policy` or `terms`; the API answers 404 for
// anything else, and 403 for a session that is not on the platform operator
// allowlist (ADR 0015).
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
