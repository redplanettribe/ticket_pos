import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// POST records that the draft's diff against the current edition was put on
// screen (#562) — the second thing #563 requires before there is a publish
// button, so that no publication happens without its consequence being shown.
//
// NO BODY. The diff is computed in the browser from the published edition and
// the draft, which the workspace read handed over together; what the API stores
// is the pair it was a diff OF, so it lapses by itself when either side moves.
// A client that could name that pair could claim to have seen a diff nobody
// drew.
export async function POST(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/draft/diff-seen`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
