import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// Check status (#455): ask the SRI again about a non-authorized Tax Invoice
// and answer with the invoice as it then stands. No body; the id is the
// whole request.
export async function POST(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorInvoiceDetail>(
      `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/check`,
      { method: "POST", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
