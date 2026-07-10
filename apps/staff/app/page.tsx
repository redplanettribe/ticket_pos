import { Card, CardContent, CardDescription, CardHeader, CardTitle, StaffShell } from "@ticket-pos/ui";
import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { LogoutButton } from "./logout-button";
import { SwitchOrganizationControl } from "./switch-organization-control";

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
    organization_name: string;
    organization_slug: string;
    role: string;
  }>;
};

async function loadSession(): Promise<SessionData | null> {
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
}

export default async function StaffDashboardPage() {
  const session = await loadSession();
  const organizationName = session?.active_member?.organization_name ?? "Ticket POS";

  return (
    <StaffShell
      organizationName={organizationName}
      activePath="/"
      userMenu={
        <div className="space-y-3">
          {session?.active_member && session.memberships.length > 1 ? (
            <SwitchOrganizationControl
              memberships={session.memberships}
              activeMemberId={session.active_member.member_id}
            />
          ) : null}
          <LogoutButton />
        </div>
      }
    >
      <div className="mx-auto max-w-4xl space-y-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">
            Catalog management, POS mode, and sale imports will live here.
          </p>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Session</CardTitle>
            <CardDescription>Your current staff session details.</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-4 sm:grid-cols-2">
              <div>
                <dt className="text-sm font-medium text-muted-foreground">Email</dt>
                <dd className="mt-1">{session?.email ?? "Unknown"}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-muted-foreground">Active organization</dt>
                <dd className="mt-1">{session?.active_member?.organization_name ?? "None selected"}</dd>
              </div>
              {session?.active_member ? (
                <div>
                  <dt className="text-sm font-medium text-muted-foreground">Role</dt>
                  <dd className="mt-1">{session.active_member.role.replace("_", " ")}</dd>
                </div>
              ) : null}
            </dl>
          </CardContent>
        </Card>
      </div>
    </StaffShell>
  );
}
