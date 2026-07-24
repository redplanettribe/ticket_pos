import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { apiErrorResponse } from "@/lib/bff";
import { clientIpHeaders } from "@/lib/client-ip";

/**
 * Requests a one-time passcode for Customer sign-in.
 *
 * This is the only public, unauthenticated write the Storefront exposes, and the
 * most attractive abuse target in the system: anyone on the internet can reach
 * it, and every accepted call sends an email from our domain. The API defends it
 * with a per-IP limit, and that limit counts whatever IP arrives in
 * X-BFF-Client-IP — so this route derives it (lib/client-ip.ts) and must never
 * forward the browser's own x-forwarded-for, which the caller controls. Passing
 * the browser's copy would let an attacker rotate the value per request and
 * never exhaust the allowance (ADR 0008).
 *
 * The API answers identically whether or not the email is known, so this route
 * has nothing to hide and simply relays the envelope.
 */
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const envelope = await callBackend<{ message: string }>(
      "/api/v1/customer/auth/otp/request",
      {
        method: "POST",
        body: JSON.stringify(body),
        headers: clientIpHeaders(request.headers),
      },
    );
    return NextResponse.json(envelope);
  } catch (error) {
    return apiErrorResponse(error);
  }
}
