import { redirect } from "next/navigation";

import { loadSession } from "../../../staff-page-shell";
import { AffiliateLinksSection } from "../affiliate-links-section";

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

  return <AffiliateLinksSection eventId={id} />;
}
