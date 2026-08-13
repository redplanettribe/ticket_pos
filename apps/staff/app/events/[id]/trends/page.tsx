import { redirect } from "next/navigation";

import { loadSession } from "../../../staff-page-shell";
import { SalesTrendsSection } from "../sales-trends-section";

type TrendsPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventTrendsPage({ params }: TrendsPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Sales Trends carries the guard the Event's money already has: Org Admins and
  // Event Owners, not hired door staff. The nav hides the tab from Event Staff,
  // so this guard is for the URL somebody was sent — and the API refuses them
  // the endpoint regardless.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/sales`);
  }

  // An Event with External Registration is not special-cased here. It sells no
  // Ticket Sale, so the endpoint returns no days and the surface shows its empty
  // state, which names that case in words. Reading registration_mode to hide the
  // tab would put the same answer in two places, and they would eventually
  // disagree — an Event switched to tickets mid-life would still be told it has
  // nothing to chart while its sales piled up.
  return <SalesTrendsSection eventId={id} />;
}
