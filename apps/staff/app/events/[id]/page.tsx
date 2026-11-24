import { redirect } from "next/navigation";

import { loadSession } from "../../staff-page-shell";
import { EventDetailForm } from "./event-detail-form";

type EventDetailPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventDetailPage({ params }: EventDetailPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Event Staff have no editable Details actions; send them straight to the
  // area they manage. The Details index is read-only reference for them.
  if (role === "event_staff") {
    redirect(`/events/${id}/ticket-types`);
  }

  // Tags are managed by org_admin / event_owner only.
  const canManageTags = role === "org_admin" || role === "event_owner";
  // The Thumbnail upload URL is Org Admin only on the server, so the card is
  // too: an Event Owner would otherwise be offered an upload that is refused.
  const canEditThumbnail = role === "org_admin";

  return <EventDetailForm eventId={id} canManageTags={canManageTags} canEditThumbnail={canEditThumbnail} />;
}
