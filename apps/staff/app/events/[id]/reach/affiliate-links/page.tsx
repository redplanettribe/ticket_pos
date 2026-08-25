import { loadEvent } from "@/lib/staff-event";

import { AffiliateLinksSection } from "../../affiliate-links-section";

type ReachAffiliateLinksPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Affiliate Links tab of the Reach surface: the management table, moved
 * whole from `/events/:id/affiliate-links` (#464). Nothing about creating,
 * renaming, deactivating or deleting a link changes but its address — the
 * chart that used to read beneath it is the surface's first tab now.
 *
 * No guard here: the Reach layout carries the Org Admin / Event Owner check
 * for both sub-tabs alike.
 */
export default async function ReachAffiliateLinksPage({ params }: ReachAffiliateLinksPageProps) {
  const { id } = await params;

  // The Event's own timezone so the Created column is drawn in the zone the
  // Organizer works in, as the Sales list's times are. Tolerates failure: the
  // list still renders, in the platform zone.
  const event = await loadEvent(id);
  const timezone = event?.timezone ?? null;

  return <AffiliateLinksSection eventId={id} timezone={timezone} />;
}
