import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import type { AccessLogPage } from "@/lib/access-log";
import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// One page of the consent access log (#569).
//
// THE QUERY STRING IS FORWARDED, which the acceptance browsers' proxies
// pointedly do not do. Theirs carry a search fragment and a cursor that IS an
// email address; these filters name an OPERATOR, an act, two dates and a keyset
// position — none of which is a data subject. The rule is no data subject in a
// request line, not "no query strings".
//
// ONE METHOD AND ONE ROUTE. There is no DELETE here and no purge behind it: the
// log's retention is unbounded by design, so evidence is never aged out.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<AccessLogPage>(`/api/v1/operator/legal/access-log${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
