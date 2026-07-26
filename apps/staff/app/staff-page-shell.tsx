import type { ReactNode } from "react";
import { cache } from "react";

import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { LogoutButton } from "./logout-button";
import { StaffShellWithOrganizationSwitcher } from "./organization-switcher-dialog";

export type SessionData = {
  email: string;
  /**
   * True when this session's email is on the platform operator allowlist
   * (ADR 0015). It decides whether the Operator Dashboard and its nav entry
   * render at all; the API remains the actual gate.
   */
  is_platform_operator: boolean;
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

type StaffPageShellProps = {
  activePath: string;
  children: ReactNode;
};

export async function StaffPageShell({ activePath, children }: StaffPageShellProps) {
  const session = await loadSession();
  const organizationName = session?.active_member?.organization_name ?? "Multiticketing";
  const organizationLogoUrl = session?.active_member?.organization_logo_url ?? null;

  return (
    <StaffShellWithOrganizationSwitcher
      organizationName={organizationName}
      organizationLogoUrl={organizationLogoUrl}
      activePath={activePath}
      showSettings={session?.active_member?.role === "org_admin"}
      showEvents={session?.active_member != null}
      showOperator={session?.is_platform_operator === true}
      memberships={session?.memberships ?? []}
      activeMemberId={session?.active_member?.member_id}
      userMenu={<LogoutButton />}
    >
      {children}
    </StaffShellWithOrganizationSwitcher>
  );
}
