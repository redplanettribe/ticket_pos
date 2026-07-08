import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { SESSION_COOKIE_NAME } from "@/lib/session";

const publicPaths = ["/login"];

const onboardingPaths = ["/onboarding"];
const orgPickerPaths = ["/select-organization"];

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
  const membershipCount = session.memberships.length;

  if (hasActiveMember) {
    if (isPathMatch(pathname, onboardingPaths) || isPathMatch(pathname, orgPickerPaths)) {
      const dashboardUrl = request.nextUrl.clone();
      dashboardUrl.pathname = "/";
      dashboardUrl.search = "";
      return NextResponse.redirect(dashboardUrl);
    }
    return NextResponse.next();
  }

  if (isPathMatch(pathname, onboardingPaths) || isPathMatch(pathname, orgPickerPaths)) {
    return NextResponse.next();
  }

  const redirectUrl = request.nextUrl.clone();
  redirectUrl.search = "";
  if (membershipCount === 0) {
    redirectUrl.pathname = "/onboarding/create-organization";
  } else {
    redirectUrl.pathname = "/select-organization";
  }
  return NextResponse.redirect(redirectUrl);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
