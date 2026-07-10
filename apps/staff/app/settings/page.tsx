import { PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";

import { SettingsPageClient } from "./settings-page-client";

export default async function SettingsPage() {
  return (
    <StaffPageShell activePath="/settings">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Settings"
          description="Manage organization profile, members, and event access."
        />
        <SettingsPageClient />
      </div>
    </StaffPageShell>
  );
}
