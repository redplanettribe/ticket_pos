import { cache } from "react";

import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * The Staff Session as this app reads it, once per request.
 *
 * This lives in lib/ rather than beside the shell it was extracted from because
 * i18n/request.ts needs it too, and importing the shell from there would drag a
 * tree of client components into next-intl's request configuration. Nothing else
 * moved: `loadSession` is still re-exported from app/staff-page-shell.tsx, which
 * is where three dozen pages already import it from.
 */

export type SessionData = {
  email: string;
  /**
   * True when this session's email is on the platform operator allowlist
   * (ADR 0015). It decides whether the Operator Dashboard and the switcher's
   * Platform entry exist at all; the API remains the actual gate.
   */
  is_platform_operator: boolean;
  /**
   * The Staff Locale of the person this session belongs to, or null when they
   * have stated none (ADR 0041).
   *
   * Null is NOT "English". It says nobody has chosen — a person invited but never
   * signed in, or one whose sign-in predates this feature — which is what lets
   * the next sign-in record a detected language rather than find one already
   * there. A reader with null is written to in English all the same.
   *
   * It sits on the session and not on `active_member` because it is a fact about
   * the PERSON: one human belonging to two Organizations has one language, and a
   * Platform Operator who belongs to none still has one. That is the whole reason
   * the API reports it here as well as on /staff/me.
   */
  locale: string | null;
  active_member: {
    member_id: string;
    organization_name: string;
    organization_slug: string;
    organization_logo_url: string | null;
    role: string;
  } | null;
  memberships: Array<{
    member_id: string;
    organization_id: string;
    organization_name: string;
    organization_slug: string;
    organization_logo_url: string | null;
    role: string;
  }>;
};

/**
 * The signed-in person, resolved once per request and shared by everything that
 * asks.
 *
 * `cache()` is what makes the Staff Locale free. i18n/request.ts asks for the
 * session to decide which language to render in, and the shell asks for the same
 * session a moment later to draw the Organization's name — one HTTP call to the
 * API, deduplicated by React for the life of the request. No blocking request is
 * added to the render path by internationalizing the app, which is the promise
 * ADR 0041 makes and the reason the API put `locale` on a call that was already
 * happening.
 *
 * A missing cookie short-circuits before any call at all, so the sign-in page —
 * the one surface a signed-out person reaches — costs exactly what it always did.
 */
export const loadSession = cache(async (): Promise<SessionData | null> => {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return null;
  }

  try {
    const envelope = await callBackend<SessionData>("/api/v1/auth/session", {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data;
  } catch {
    return null;
  }
});
