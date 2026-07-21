import { redirect } from "next/navigation";

import { loadSession } from "../../../staff-page-shell";
import { EventTagsSection } from "../event-tags-section";

type EventTagsPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventTagsPage({ params }: EventTagsPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Tags are managed by org_admin / event_owner only. The nav hides this area
  // from Event Staff; guard the route since it stays directly reachable by URL.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/ticket-types`);
  }

  return <EventTagsSection eventId={id} />;
}
