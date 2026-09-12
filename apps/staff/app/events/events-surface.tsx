import { StaffPageShell, loadSession } from "../staff-page-shell";
import type { EventsTabKey } from "@/lib/events-tabs";

import { EventsPageClient } from "./events-page-client";

type EventsSurfaceProps = {
  /** Which tab the calling route is. */
  tab: EventsTabKey;
};

/**
 * The Events list surface, on one of its three tabs (#613).
 *
 * Each tab is a route of its own — `/events`, `/events/past`,
 * `/events/cancelled` — so it can be bookmarked and the back button moves
 * between tabs as between pages (ADR 0071). The three route files differ by one
 * constant, and everything else they would have said three times lives here:
 * the session read that decides whether the reader is an Org Admin, the shell,
 * and the one client component that fetches the list and splits it.
 *
 * The shell's active path is the Events entry's own href on all three, because
 * a sidebar entry owns its whole subtree (`isNavItemActive`) and a reader on the
 * Past tab has not left Events.
 */
export async function EventsSurface({ tab }: EventsSurfaceProps) {
  const session = await loadSession();
  const isOrgAdmin = session?.active_member?.role === "org_admin";

  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <EventsPageClient isOrgAdmin={isOrgAdmin} tab={tab} />
      </div>
    </StaffPageShell>
  );
}
