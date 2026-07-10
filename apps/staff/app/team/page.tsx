import { InProgressPanel, PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";

export default async function TeamPage() {
  return (
    <StaffPageShell activePath="/team">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Team"
          description="Manage members, invites, and Event assignments."
        />
        <InProgressPanel
          title="Team management is under development"
          description="Invite members, assign roles, and control who can sell at each Event."
        />
      </div>
    </StaffPageShell>
  );
}
