import { InProgressPanel, PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";

export default async function EventsPage() {
  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Events"
          description="Create and manage Events and Ticket Types."
        />
        <InProgressPanel
          title="Event catalog is under development"
          description="You'll create Events, set dates and venues, and configure Ticket Types from here."
        />
      </div>
    </StaffPageShell>
  );
}
