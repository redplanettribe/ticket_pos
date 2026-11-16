import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// Mark annulled (#477, #745): record that the operator annulled the
// document by hand at the SRI portal, and answer with the document as it
// then stands. On a factura the body is the confirm - the number typed in
// full, the note, the late acknowledgement - and goes through as sent; a
// Credit Note's press has none, and none is forwarded. Who annulled is the
// session's, and the Go API takes it from there. The refusals are forwarded
// by code.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  try {
    const body = await request.text();
    const envelope = await callBackend<OperatorInvoiceDetail>(
      `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/annul`,
      { method: "POST", sessionToken: token, ...(body ? { body } : {}) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
