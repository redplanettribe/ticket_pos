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

  return <EventDetailForm eventId={id} />;
}
