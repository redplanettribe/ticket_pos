import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorOrganization } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// PUT designates the Organization a House Organization (#472, ADR 0060): one
// the platform's own entity runs, whose paid online sales the platform's Issuer
// will invoice. No body — the API stamps who and when from the session. Refused
// for an Organization trading in a currency other than USD, and for anyone not
// on the platform operator allowlist (ADR 0015).
export async function PUT(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorOrganization>(
      `/api/v1/operator/organizations/${id}/house`,
      { method: "PUT", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// DELETE clears the designation, emptying who and when together. Nothing
// already owed or issued is touched. No body.
export async function DELETE(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorOrganization>(
      `/api/v1/operator/organizations/${id}/house`,
      { method: "DELETE", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
