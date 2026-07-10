import { InProgressPanel, PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";

export default async function ImportsPage() {
  return (
    <StaffPageShell activePath="/imports">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Imports"
          description="Upload CSV sale imports for an Event."
        />
        <InProgressPanel
          title="Sale imports are under development"
          description="Upload CSV files from external platforms and reconcile sales against your Events."
        />
      </div>
    </StaffPageShell>
  );
}
