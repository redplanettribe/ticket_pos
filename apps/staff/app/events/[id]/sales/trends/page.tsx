import { redirect } from "next/navigation";

import { loadSession } from "../../../../staff-page-shell";
import { SalesTrendsSection } from "../../sales-trends-section";

type SalesTrendsPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Trends tab: the Sales Trends section, moved whole from `/events/:id/trends`
 * (#460). Nothing about the chart changes but its address — it now reads under
 * the Sales layout, so the Net Proceeds strip and the tab strip stand above it
 * as they do over the list and the recording tools, and the same chart is no
 * longer offered from the Event panel as well.
 */
export default async function SalesTrendsPage({ params }: SalesTrendsPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Sales Trends carries the guard the Event's money already has: Org Admins and
  // Event Owners, not hired door staff. The tab strip hides the tab from Event
  // Staff, so this guard is for the URL somebody was sent — and the API refuses
  // them the endpoint regardless. A redirect to the list rather than a refusal,
  // the way this page has always answered it.
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
