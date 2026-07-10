import type { ReactNode } from "react";
import { cache } from "react";

import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { LogoutButton } from "./logout-button";
import { StaffShellWithOrganizationSwitcher } from "./organization-switcher-dialog";

type SessionData = {
  email: string;
  active_member: {
    member_id: string;
    organization_name: string;
    organization_slug: string;
    role: string;
  } | null;
  memberships: Array<{
    member_id: string;
    organization_id: string;
    organization_name: string;
    organization_slug: string;
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
  const organizationName = session?.active_member?.organization_name ?? "Ticket POS";

  return (
    <StaffShellWithOrganizationSwitcher
      organizationName={organizationName}
      activePath={activePath}
      showSettings={session?.active_member?.role === "org_admin"}
      memberships={session?.memberships ?? []}
      activeMemberId={session?.active_member?.member_id}
      userMenu={<LogoutButton />}
    >
      {children}
    </StaffShellWithOrganizationSwitcher>
  );
}
