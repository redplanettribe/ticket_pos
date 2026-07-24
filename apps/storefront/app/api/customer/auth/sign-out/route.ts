import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import {
  CUSTOMER_SESSION_COOKIE,
  clearedCustomerSessionCookieOptions,
} from "@/lib/customer-session";

/**
 * Signs the visitor out of the Storefront.
 *
 * Two things happen and both matter: the API destroys the server-side Customer
 * Session row, so the token is worthless even if it were somehow copied, and this
 * app erases its own cookie. The cookie is erased even when the API call fails,
 * because a visitor who asked to sign out on a shared device must end up signed
 * out here regardless of what the API said.
 *
 * Nothing here can affect a Staff Session. That session lives in a different
 * cookie on a different origin, backed by a different table this endpoint cannot
 * name (ADR 0010) — a promoter signing out of the Storefront keeps their box
 * office.
 */
export async function POST() {
  const store = await cookies();
  const token = store.get(CUSTOMER_SESSION_COOKIE)?.value;

  if (token) {
    try {
      await callBackend<{ message: string }>("/api/v1/customer/auth/logout", {
        method: "POST",
        sessionToken: token,
      });
    } catch {
      // Already expired, already destroyed, or the API is unreachable. The
      // visitor's intent is the same either way: clear the cookie below.
    }
  }

  store.set(CUSTOMER_SESSION_COOKIE, "", clearedCustomerSessionCookieOptions());
  return NextResponse.json({
    data: { message: "Signed out" },
    error: null,
    request_id: crypto.randomUUID(),
  });
}
