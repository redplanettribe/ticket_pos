import { cookies } from "next/headers";

import { unauthorizedResponse } from "@/lib/bff";
import { proxyInvoiceDocument } from "@/lib/invoice-document-proxy";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// GET streams a Tax Invoice's signed XML from the Go API through the BFF
// (#456), preserving the download headers so the browser saves the file under
// the clave. Available in every status.
export async function GET(_request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  return proxyInvoiceDocument(`/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/xml`, token);
}
