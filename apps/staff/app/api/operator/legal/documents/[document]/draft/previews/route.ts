import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorLegalWorkspace } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ document: string }>;
};

// POST records that one artifact of the draft was seen rendered, in one language
// (#562).
//
// THERE IS NOTHING TO RENDER HERE. The preview is drawn in the browser by the
// same `Markdown` component the Storefront's privacy-policy page uses, over text
// the workspace read already carried; this proxy forwards only the RECORD that
// somebody looked, `{slug, locale}`, with no text in it. That is what keeps the
// preview off the public route, which resolves what is current itself and
// refuses to be told which edition to serve — an unpublished edition must not be
// reachable by asking a reader's URL nicely.
//
// The API answers 409 for a document with no saved draft: a preview promises a
// look at the text that WILL be published, and unsaved text will not be.
export async function POST(request: Request, context: RouteContext) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { document } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<OperatorLegalWorkspace>(
      `/api/v1/operator/legal/documents/${encodeURIComponent(document)}/draft/previews`,
      { method: "POST", sessionToken: token, body: JSON.stringify(body) },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
