import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { CustomerAcceptancePage } from "@/lib/acceptance-browsers";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// One page of the customer acceptance browser (#565, spec #556, ADR 0067).
//
// A POST THAT READS, and the shape is deliberate rather than sloppy: nothing is
// created, recorded or changed. What it must not do is put a data subject's
// email into a URL, a query string or a referer, and TWO of the parameters are
// addresses — the search fragment, and the CURSOR, which under keyset paging on
// `email ASC` is the last address of the previous page. Every other operator
// proxy in this app forwards `new URL(request.url).search`; this one carries
// nothing in a query string at all, and forwards the body instead.
//
// The DOCUMENT is in the path, which is fine: `policy` or `terms` is the name
// of a public agreement and nobody's personal data. The API answers 404 for
// anything else, and 403 for a session that is not on the platform operator
// allowlist (ADR 0015).
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<CustomerAcceptancePage>(
      `/api/v1/operator/legal/acceptances/customers/${encodeURIComponent(document)}`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
