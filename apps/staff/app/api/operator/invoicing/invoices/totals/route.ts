import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorInvoiceTotals } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// The server's totals for a set of lines (#454): the arithmetic the document
// will carry, so the form shows it rather than computing its own.
const BACKEND_PATH = "/api/v1/operator/invoicing/invoices/totals";

export async function POST(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorInvoiceTotals>(BACKEND_PATH, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
