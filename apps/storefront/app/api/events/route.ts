import { NextResponse } from "next/server";

import { listPublicEvents } from "@/lib/api";

// Server-side proxy so the client "Load more" control can page through the
// global explorer without knowing the Go API URL.
export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const limit = params.get("limit");
  const page = await listPublicEvents({
    q: params.get("q") ?? undefined,
    from: params.get("from") ?? undefined,
    to: params.get("to") ?? undefined,
    cursor: params.get("cursor") ?? undefined,
    limit: limit ? Number(limit) : undefined,
  });

  if (!page) {
    return NextResponse.json({ events: [], next_cursor: null }, { status: 502 });
  }
  return NextResponse.json(page);
}
