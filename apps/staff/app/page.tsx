import { Card, CardContent, CardDescription, CardHeader, CardTitle, PageHeader } from "@ticket-pos/ui";

import { loadSession, StaffPageShell } from "./staff-page-shell";

export default async function StaffDashboardPage() {
  const session = await loadSession();

  return (
    <StaffPageShell activePath="/">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Dashboard"
          description="Catalog management, POS mode, and sale imports will live here."
        />

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
    </StaffPageShell>
  );
}
