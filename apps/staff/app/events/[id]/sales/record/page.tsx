import { redirect } from "next/navigation";

import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../staff-page-shell";
import { ImportSalesSection } from "../../import-sales-section";

type RecordSalesPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Record tab: the "Record sales" card, moved whole from the foot of the
 * Sales list. Nothing about recording a sale changes but its address — the
 * Manually Recorded Sale dialog, the Sale Import upload and its history are the
 * same component they always were.
 */
export default async function RecordSalesPage({ params }: RecordSalesPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Recording a sale carries the guard the Event's money already has: Org Admins
  // and Event Owners, not hired door staff. The tab strip hides the tab from
  // Event Staff, so this guard is for the URL somebody was sent — and the API
  // refuses them the endpoints regardless. A redirect to the list rather than a
  // refusal, the way the Trends page has always answered it.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/sales`);
  }

  // The Event's timezone reaches the import history for the reason it reaches
  // the list: an imported batch happened at a moment, and the moment is drawn on
  // the Event's clock rather than on the reader's machine, whichever language
  // the reader is in (ADR 0041). The read is the request-shared one the Event
  // layout has already made, so it costs no call of its own; a failure leaves
  // the times in the viewer's local zone rather than losing the card.
  const event = await loadEvent(id);

  return <ImportSalesSection eventId={id} timezone={event?.timezone ?? null} />;
}
