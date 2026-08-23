import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

type RouteContext = {
  params: Promise<{ id: string }>;
};

async function sessionToken() {
  const cookieStore = await cookies();
  return cookieStore.get(SESSION_COOKIE_NAME)?.value;
}

// GET returns a page of the Event's Sales list (ADR-0006 nested { data,
// pagination } envelope), forwarding the pagination query string to the Go API.
// Readable by any Member of the Event.
export async function GET(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  const search = new URL(request.url).search;
  try {
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/sales${search}`, {
      method: "GET",
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    return jsonFromAPIError(error);
  }
}

// POST records one Manually Recorded Sale (#369, ADR 0052): the Sale Import
// template's columns as JSON, forwarded verbatim to the Go endpoint, whose 201
// — the created sale and its Sale Confirmation reference — comes back as is.
//
// The method split is the gate split: the GET above is readable by every Member
// of the Event, while writing a sale is refused to anyone who cannot manage the
// Event's sales. The browser talks only to the BFF; the session is attached here.
export async function POST(request: Request, context: RouteContext) {
  const token = await sessionToken();
  if (!token) {
    return unauthorizedResponse();
  }

  const { id } = await context.params;
  try {
    const body = await request.json();
    const envelope = await callBackend<unknown>(`/api/v1/staff/events/${id}/sales`, {
      method: "POST",
      sessionToken: token,
      body: JSON.stringify(body),
    });
    return NextResponse.json(envelope, { status: 201 });
  } catch (error) {
    return jsonFromAPIError(error);
  }
}
