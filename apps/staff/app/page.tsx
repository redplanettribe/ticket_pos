import { PageHeader } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { SessionCard } from "./session-card";
import { loadSession, StaffPageShell } from "./staff-page-shell";

export default async function StaffDashboardPage() {
  const t = await getTranslations("shell");
  const session = await loadSession();

  return (
    <StaffPageShell activePath="/">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader title={t("dashboardTitle")} description={t("dashboardDescription")} />

        <SessionCard
          email={session?.email ?? null}
          organizationName={session?.active_member?.organization_name ?? null}
          role={session?.active_member?.role ?? null}
        />
      </div>
    </StaffPageShell>
  );
}
