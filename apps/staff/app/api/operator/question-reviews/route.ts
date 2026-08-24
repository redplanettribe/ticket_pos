import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import type { OperatorQuestionReviewQueuePage } from "@/lib/operator-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

// GET lists every outstanding Question Review across every organization,
// oldest first (#407, ADR 0056). Reviews whose event has started are lapsed
// on this read, so nothing listed is past answering.
export async function GET(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  const { searchParams } = new URL(request.url);
  const params = new URLSearchParams();
  for (const key of ["page", "page_size"]) {
    const value = searchParams.get(key);
    if (value) {
      params.set(key, value);
    }
  }
  const query = params.toString();

  try {
    const envelope = await callBackend<OperatorQuestionReviewQueuePage>(
      `/api/v1/operator/question-reviews${query ? `?${query}` : ""}`,
      { method: "GET", sessionToken: token },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
