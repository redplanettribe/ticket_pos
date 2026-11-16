import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// Reinstate (#749, ADR 0078): return an annulled factura to authorized once
// its window has closed with the SRI still reporting it authorized, and
// answer with the document as it then stands. The confirm - the number typed
// in full and the optional note - goes through as sent; who reinstated is
// the session's. The refusals are forwarded by code.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorInvoiceDetail>(
      `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/reinstate`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
