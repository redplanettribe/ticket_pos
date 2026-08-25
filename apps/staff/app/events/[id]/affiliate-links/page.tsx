import { redirect } from "next/navigation";

import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../staff-page-shell";
import { AffiliateLinksSection } from "../affiliate-links-section";
import { AffiliateTrendsSection } from "../affiliate-trends-section";

type AffiliateLinksPageProps = {
  params: Promise<{ id: string }>;
};

export default async function AffiliateLinksPage({ params }: AffiliateLinksPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Affiliate Links are managed by org_admin / event_owner only. The nav hides
  // this area from Event Staff; guard the route since it stays directly
  // reachable by URL — and the API refuses them regardless.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/ticket-types`);
  }

  // The Event's own timezone so the Created column is drawn in the zone the
  // Organizer works in, as the Sales list's times are. Tolerates failure: the
  // list still renders, in the platform zone.
  const event = await loadEvent(id);
  const timezone = event?.timezone ?? null;

  // The management list first — creating a link is the tab's first job — and
  // the trends chart beneath it, reading what the links above have done.
  return (
    <div className="space-y-6">
      <AffiliateLinksSection eventId={id} timezone={timezone} />
      <AffiliateTrendsSection eventId={id} />
    </div>
  );
}
