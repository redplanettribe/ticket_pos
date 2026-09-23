import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { ConsentActPage } from "@/lib/legal-records";
import { proxyRead } from "@/lib/reader-abort";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ customerId: string }>;
};

// One page of one Customer's consent history (#566).
//
// ITS OWN ROUTE rather than a cursor folded into the record read, so the
// landing request and page 2 return the same shape and "load more" is a request
// that can be replayed on its own.
//
// THE QUERY STRING IS FORWARDED, which the acceptance browsers' proxies
// pointedly do not do. Theirs carry a cursor that IS an email address, keyset
// on `email ASC`; this one carries a capture timestamp and a row id, which name
// nobody. The rule is no email in a request line, not "no query strings".
export async function GET(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { customerId } = await context.params;
  const search = new URL(request.url).search;
  return proxyRead(
    request,
    async (signal) => {
      const envelope = await callBackend<ConsentActPage>(
        `/api/v1/operator/legal/customers/${encodeURIComponent(customerId)}/records${search}`,
        { method: "GET", sessionToken: token, signal },
      );
      return NextResponse.json(envelope);
    },
    jsonFromAPIError,
  );
}
