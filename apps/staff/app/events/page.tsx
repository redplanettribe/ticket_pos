import { StaffPageShell } from "../staff-page-shell";
import { EventsPageClient } from "./events-page-client";

export default async function EventsPage() {
  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <EventsPageClient />
      </div>
    </StaffPageShell>
  );
}
