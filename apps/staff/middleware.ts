import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { resolveAuthForkRedirectPath } from "@/lib/auth-fork";
import { SESSION_COOKIE_NAME } from "@/lib/session";

const publicPaths = ["/login"];

const createOrganizationPaths = ["/organizations/new"];
const orgPickerPaths = ["/select-organization"];
const legacyOnboardingPaths = ["/onboarding"];

type SessionData = {
  email: string;
  active_member: { member_id: string; organization_name?: string } | null;
  memberships: Array<{ member_id: string; organization_name?: string }>;
};

type SessionEnvelope = {
  data: SessionData | null;
  error: { code: string } | null;
};

function isPathMatch(pathname: string, paths: string[]): boolean {
  return paths.some((path) => pathname === path || pathname.startsWith(`${path}/`));
}

async function fetchSession(request: NextRequest): Promise<SessionData | null> {
  const sessionUrl = new URL("/api/auth/session", request.url);
  const response = await fetch(sessionUrl, {
    headers: {
      cookie: request.headers.get("cookie") ?? "",
    },
  });
  if (!response.ok) {
    return null;
  }
  const envelope = (await response.json()) as SessionEnvelope;
  return envelope.data;
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

  const session = await fetchSession(request);
  if (!session) {
    const loginUrl = request.nextUrl.clone();
    loginUrl.pathname = "/login";
    loginUrl.search = "";
    const response = NextResponse.redirect(loginUrl);
    response.cookies.delete(SESSION_COOKIE_NAME);
    return response;
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
  redirectUrl.pathname = resolveAuthForkRedirectPath(session);
  return NextResponse.redirect(redirectUrl);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
