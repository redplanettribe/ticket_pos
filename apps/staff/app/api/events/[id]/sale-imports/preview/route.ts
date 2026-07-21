import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { fetchBackendRaw } from "@/lib/api";
import { unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// POST forwards the uploaded .csv/.xlsx (multipart form-data) to the Go preview
// endpoint and returns its JSON envelope unchanged. The browser talks only to
// the BFF; the session is attached here.
export async function POST(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const form = await request.formData();

  const upstream = await fetchBackendRaw(`/api/v1/staff/events/${id}/sale-imports/preview`, {
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
