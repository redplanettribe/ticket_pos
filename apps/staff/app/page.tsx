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

  return (
    <main>
      <h1>Staff Dashboard</h1>
      <p>Catalog management, POS mode, and sale imports will live here.</p>

      <section aria-label="Session">
        <h2>Session</h2>
        <dl>
          <div>
            <dt>Email</dt>
            <dd>{session?.email ?? "Unknown"}</dd>
          </div>
          <div>
            <dt>Active organization</dt>
            <dd>{session?.active_member?.organization_name ?? "None selected"}</dd>
          </div>
          {session?.active_member ? (
            <div>
              <dt>Role</dt>
              <dd>{session.active_member.role.replace("_", " ")}</dd>
            </div>
          ) : null}
        </dl>
        {session?.active_member && session.memberships.length > 1 ? (
          <SwitchOrganizationControl
            memberships={session.memberships}
            activeMemberId={session.active_member.member_id}
          />
        ) : null}
      </section>

      <nav>
        <LogoutButton />
      </nav>
    </main>
  );
}
