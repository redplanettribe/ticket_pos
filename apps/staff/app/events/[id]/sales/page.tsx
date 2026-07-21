import { redirect } from "next/navigation";

import { loadSession } from "../../../staff-page-shell";
import { ImportSalesSection } from "../import-sales-section";

type EventSalesPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventSalesPage({ params }: EventSalesPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Off-platform Sale Import is managed by org_admin / event_owner only. The nav
  // hides this area from Event Staff; guard the route since it stays directly
  // reachable by URL.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/ticket-types`);
  }

  return <ImportSalesSection eventId={id} />;
}
