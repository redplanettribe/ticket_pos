import { cookies } from "next/headers";

import { unauthorizedResponse } from "@/lib/bff";
import { proxyInvoiceDocument } from "@/lib/invoice-document-proxy";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

// GET streams the SRI's authorization XML for an authorized Tax Invoice
// (#456). Anything else answers the API's not-found envelope, passed through.
export async function GET(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }
  const { id } = await context.params;
  return proxyInvoiceDocument(
    request,
    `/api/v1/operator/invoicing/invoices/${encodeURIComponent(id)}/authorization-xml`,
    token,
  );
}
