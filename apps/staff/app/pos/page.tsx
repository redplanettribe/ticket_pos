import { InProgressPanel, PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";

export default async function POSPage() {
  return (
    <StaffPageShell activePath="/pos">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="POS"
          description="In-person sales on a tablet."
        />
        <InProgressPanel
          title="POS mode is under development"
          description="Pick an Event, select Ticket Types, and confirm payment at the door."
        />
      </div>
    </StaffPageShell>
  );
}
