import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string; version: string }>;
};

// POST cancels a scheduled edition (#564): a publication that has been made but
// has not taken effect yet, withdrawn during the night the overnight delay buys.
//
// A THIN RELAY, and thinner than most: it forwards no body, because there is
// none. Cancelling is ungated and immediate — no reason, no delay, no
// confirmation ceremony and no approval step — since undoing is always cheaper
// than doing, and every rule that remains belongs to the API: whether the
// edition exists, whether its day has passed, and whether the caller is a
// Platform Operator. Who withdrew it is taken from the Staff Session on the far
// side and never from here.
//
// POST .../cancel and not DELETE, because NOTHING IS DELETED: the row is
// retained and marked so the record of what was nearly published survives and
// its label stays spent.
export async function POST(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document, version } = await context.params;
  try {
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/publications/${encodeURIComponent(version)}/cancel`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
