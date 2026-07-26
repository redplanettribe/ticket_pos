import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { APIError, callBackend } from "@/lib/api";
import { resolveAuthForkRedirectPath } from "@/lib/auth-fork";
import { SESSION_COOKIE_NAME } from "@/lib/session";

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

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (pathname.startsWith("/api/")) {
    return NextResponse.next();
  }
  if (publicPaths.some((path) => pathname === path || pathname.startsWith(`${path}/`))) {
    return NextResponse.next();
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
  // A pure operator has no Membership to fork on, so the create-organization
  // prompt would be a dead end. Land them on the dashboard they actually have.
  redirectUrl.pathname = session.is_platform_operator
    ? "/operator"
    : resolveAuthForkRedirectPath(session);
  return NextResponse.redirect(redirectUrl);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
