import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { APIError, callBackend } from "@/lib/api";
import { signedInLandingPath } from "@/lib/auth-fork";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import { decideTermsGate } from "@/lib/terms-gate";

const publicPaths = ["/login"];

const createOrganizationPaths = ["/organizations/new"];
const orgPickerPaths = ["/select-organization"];
const legacyOnboardingPaths = ["/onboarding"];
const operatorPaths = ["/operator"];

type SessionData = {
  email: string;
  /** True when this session's email is on the platform operator allowlist. */
  is_platform_operator?: boolean;
  active_member: { member_id: string; organization_name?: string } | null;
  memberships: Array<{ member_id: string; organization_name?: string }>;
  /**
   * Whether this session's holder owes a Terms Acceptance of an edition that
   * still satisfies the gate (#570, ADR 0067). Only this route computes it;
   * null means "not asked" or "the read failed", and neither diverts anybody —
   * see lib/terms-gate.
   */
  terms_outstanding?: boolean | null;
};

function isPathMatch(pathname: string, paths: string[]): boolean {
  return paths.some((path) => pathname === path || pathname.startsWith(`${path}/`));
}

// Resolve the session by calling the Go API directly — the same call
// app/api/auth/session/route.ts makes — rather than fetching this app's own
// /api/auth/session route. The middleware must never hairpin a fetch to its own
// public URL: on Cloud Run that self-call fails at the TLS layer
// (ERR_SSL_WRONG_VERSION_NUMBER) and 500s every authenticated request. callBackend
// carries the OIDC service token (ADR 0008) and is runtime-agnostic.
async function fetchSession(sessionToken: string): Promise<SessionData | null> {
  try {
    const envelope = await callBackend<SessionData>("/api/v1/auth/session", {
      method: "GET",
      sessionToken,
    });
    return envelope.data;
  } catch (error) {
    // A rejected session (stale/invalid cookie → 401) is an expected outcome:
    // the caller redirects to /login and clears the cookie. A genuine transport
    // or auth-token failure is different, so let it surface rather than masquerade
    // as "signed out".
    if (error instanceof APIError) {
      return null;
    }
    throw error;
  }
}

/**
 * The sign-in page, which is public but not indifferent to who is asking.
 *
 * A visitor already holding a Staff Session must not be shown the sign-in form.
 * Until the Storefront's "Create an event" invitation (#259) put a button
 * pointing straight at /login, essentially nothing led anyone here while signed
 * in, so the form rendering unconditionally went unnoticed. Through that button
 * it became the common case, and it reads as though the staff app had signed
 * the person out — when in truth their session was never consulted. The cookie
 * is sent (SameSite=Lax, and this is a top-level navigation); nothing looked.
 *
 * The session is resolved only when a cookie is actually present, so the
 * ordinary signed-out arrival — still the overwhelming majority, and the whole
 * point of the invitation — costs exactly what it did before: no API call.
 */
async function handleSignInPage(request: NextRequest) {
  const sessionToken = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  if (!sessionToken) {
    return NextResponse.next();
  }

  let session: SessionData | null;
  try {
    session = await fetchSession(sessionToken);
  } catch {
    // Everywhere else a transport failure surfaces rather than masquerading as
    // "signed out", because there it would silently downgrade authority. Here
    // it must not: 500 on /login breaks the one page a person uses to recover
    // from anything, and the fallback grants nothing — it shows the form.
    return NextResponse.next();
  }

  if (!session) {
    // A stale or rejected cookie: the form is already the right page, but the
    // dead cookie should not survive to be re-sent on every later request.
    const response = NextResponse.next();
    response.cookies.delete(SESSION_COOKIE_NAME);
    return response;
  }

  const landingUrl = request.nextUrl.clone();
  landingUrl.pathname = signedInLandingPath(session);
  // `intent` selected the copy of a card this visitor is no longer being shown,
  // and every other redirect here drops the query too.
  landingUrl.search = "";
  return NextResponse.redirect(landingUrl);
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (pathname.startsWith("/api/")) {
    return NextResponse.next();
  }
  if (isPathMatch(pathname, publicPaths)) {
    return handleSignInPage(request);
  }

  if (isPathMatch(pathname, legacyOnboardingPaths)) {
    const createUrl = request.nextUrl.clone();
    createUrl.pathname = "/organizations/new";
    createUrl.search = "";
    return NextResponse.redirect(createUrl);
  }

  const sessionToken = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  if (!sessionToken) {
    const loginUrl = request.nextUrl.clone();
    loginUrl.pathname = "/login";
    loginUrl.search = "";
    return NextResponse.redirect(loginUrl);
  }

  const session = await fetchSession(sessionToken);
  if (!session) {
    const loginUrl = request.nextUrl.clone();
    loginUrl.pathname = "/login";
    loginUrl.search = "";
    const response = NextResponse.redirect(loginUrl);
    response.cookies.delete(SESSION_COOKIE_NAME);
    return response;
  }

  // The Terms gate, on the LIVE session (#570, ADR 0067's amendment to ADR
  // 0066). It sits here, after the session is resolved and BEFORE both the
  // operator fork and the membership fork, because everybody on this platform
  // owes the same acceptance: the operator who published the edition meets
  // their own interstitial, and somebody who is a Member of nothing is not
  // waved through on their way to the create-organization page.
  //
  // The decision itself is lib/terms-gate's, tested there; this is the
  // plumbing. Note what is NOT here: nothing is revoked, nothing re-minted, the
  // session cookie is untouched and no passcode is spent. A publish revokes no
  // session in either population and there is no control that offers to.
  //
  // `/api/` never reaches this line — the short-circuit at the top of this
  // function returns before any session is even resolved — so a sale in
  // progress commits and no in-flight mutation is refused.
  const termsGate = decideTermsGate({
    pathname,
    search: request.nextUrl.search,
    termsOutstanding: session.terms_outstanding,
  });
  if (termsGate.kind === "interstitial") {
    const gateUrl = request.nextUrl.clone();
    gateUrl.pathname = termsGate.pathname;
    gateUrl.search = termsGate.search;
    return NextResponse.redirect(gateUrl);
  }

  // Operator authority is orthogonal to Membership (ADR 0015): an operator with
  // no Organization must reach the dashboard without being diverted into the
  // create-organization fork below. A non-operator is sent to the app root —
  // the surface is invisible rather than merely forbidden.
  if (isPathMatch(pathname, operatorPaths)) {
    if (session.is_platform_operator) {
      return NextResponse.next();
    }
    const homeUrl = request.nextUrl.clone();
    homeUrl.pathname = "/";
    homeUrl.search = "";
    return NextResponse.redirect(homeUrl);
  }

  const hasActiveMember = Boolean(session.active_member);

  if (hasActiveMember) {
    if (isPathMatch(pathname, orgPickerPaths)) {
      const dashboardUrl = request.nextUrl.clone();
      dashboardUrl.pathname = "/";
      dashboardUrl.search = "";
      return NextResponse.redirect(dashboardUrl);
    }
    return NextResponse.next();
  }

  if (isPathMatch(pathname, createOrganizationPaths) || isPathMatch(pathname, orgPickerPaths)) {
    return NextResponse.next();
  }

  const redirectUrl = request.nextUrl.clone();
  redirectUrl.search = "";
  redirectUrl.pathname = signedInLandingPath(session);
  return NextResponse.redirect(redirectUrl);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
