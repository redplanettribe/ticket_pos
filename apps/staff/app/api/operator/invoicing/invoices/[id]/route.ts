import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceDetail } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// One Tax Invoice in full (#454): Recipient, Issuer snapshot, lines, totals,
// the SRI's messages verbatim, the authorization number and date once
// authorized, and the attempts ledger.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  try {
    const envelope = await callBackend<OperatorInvoiceDetail>(
      `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
