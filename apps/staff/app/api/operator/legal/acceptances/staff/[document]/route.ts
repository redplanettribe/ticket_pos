import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { StaffAcceptancePage } from "@/lib/acceptance-browsers";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// One page of the staff acceptance browser (#565, spec #556, ADR 0067).
//
// Its customer neighbour's shape and its reasons: a POST that reads, with every
// parameter in the body because two of them are email addresses — the search
// fragment, and the cursor, which under keyset paging on `email ASC` IS an
// address.
//
// Each row comes back with a `digest` rather than an id, because a staff person
// has no id: the person key of the Staff platform is an email, and an address
// must never appear in a URL. THE DIGEST PASSES STRAIGHT THROUGH THIS PROXY AND
// IS NEVER LOGGED HERE OR ANYWHERE — it is a URL key and a screen label, and
// nothing may write it to a row, a log, a file or an export.
//
// `document` is `terms` and nothing else: there is exactly one staff gate, and
// staff accept no Privacy Policy. The API answers 404 for anything else, and
// 503 on a deployment with no link secret, where the screen refuses to serve
// rather than name people under an empty key.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<StaffAcceptancePage>(
      `/api/v1/operator/legal/acceptances/staff/${encodeURIComponent(document)}`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
