import { StaffPageShell, loadSession } from "../staff-page-shell";
import { EventsPageClient } from "./events-page-client";

export default async function EventsPage() {
  const session = await loadSession();
  const isOrgAdmin = session?.active_member?.role === "org_admin";

  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <EventsPageClient isOrgAdmin={isOrgAdmin} />
      </div>
    </StaffPageShell>
  );
}
