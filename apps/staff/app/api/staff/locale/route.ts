import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { jsonFromAPIError, unauthorizedResponse } from "@/lib/bff";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * Where the shell's language switcher writes the Staff Locale.
 *
 * The BFF holds the session token — it is an httpOnly cookie the browser cannot
 * read (ADR 0011) — so a client component cannot call the Go API directly and
 * this thin proxy is how every authenticated write in this app is made.
 *
 * It gates on a session and on nothing else, which mirrors the API endpoint
 * behind it and is the point of ADR 0041's placement decision: the Staff Locale
 * belongs to a person, so an Event Staff member changes their own without an Org
 * Admin's permission, and a Platform Operator who is a Member of no Organization
 * changes theirs with no Organization in the request at all. There is no active
 * member to resolve here and no role to check.
 *
 * The body is forwarded rather than validated. The API refuses a language the
 * platform does not serve with a field error, and a second opinion here would be
 * a second place to update the day a third language arrives — which the switcher
 * cannot send anyway, since it renders one button per `LOCALES` entry.
 */
export async function PUT(request: Request) {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return unauthorizedResponse();
  }

  try {
    const body = await request.json();
    const envelope = await callBackend<{ locale: string }>("/api/v1/staff/me/locale", {
      method: "PUT",
      body: JSON.stringify(body),
      sessionToken: token,
    });
    return NextResponse.json(envelope);
  } catch (error) {
    // The envelope's `code` survives to the browser, which is what lets the
    // switcher resolve a sentence through lib/api-errors.ts rather than showing
    // the API's English (ADR 0023).
    return jsonFromAPIError(error);
  }
}
