import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME, sessionCookieOptions } from "@/lib/session";

export async function POST() {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;

  if (token) {
    try {
      await callBackend<{ message: string }>("/api/v1/auth/logout", {
        method: "POST",
        sessionToken: token,
      });
    } catch {
      // Clear the cookie even if backend logout fails.
    }
  }

  cookieStore.set(SESSION_COOKIE_NAME, "", { ...sessionCookieOptions(), maxAge: 0 });
  return NextResponse.json({
    data: { message: "Signed out" },
    error: null,
    request_id: crypto.randomUUID(),
  });
}
