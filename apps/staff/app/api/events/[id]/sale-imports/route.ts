import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend, fetchBackendRaw } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// GET returns the Event's Sale Import history (newest first) as a JSON envelope.
export async function GET(_request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/sale-imports`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// POST forwards the reviewed multipart batch (file + idempotency_key + source +
// optional skip_rows) to the Go commit endpoint and returns its JSON envelope,
// preserving the status (201 committed, 200 replayed, 409 oversell race).
export async function POST(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const form = await request.formData();

  const upstream = await fetchBackendRaw(`/api/v1/staff/events/${id}/sale-imports`, {
    method: "POST",
    sessionToken: token,
    body: form,
  });

  const body = await upstream.text();
  return new NextResponse(body, {
    status: upstream.status,
    headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
  });
}
