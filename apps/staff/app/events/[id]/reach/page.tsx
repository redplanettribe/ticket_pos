import { AffiliateTrendsSection } from "../affiliate-trends-section";

type ReachTrendsPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Trends tab, and the index of the Reach surface: Reach Trends, the chart
 * that was Link Trends beneath the Affiliate Links table until #464. Nothing
 * about the chart changes but its address and its words — it draws the page's
 * views against each link's clicks exactly as before, and now leads the surface
 * because the surface is about the page, with the links one part of that.
 *
 * No guard here: the Reach layout carries the Org Admin / Event Owner check
 * for both sub-tabs alike, so repeating it would put the same rule in two
 * places.
 */
export default async function ReachTrendsPage({ params }: ReachTrendsPageProps) {
  const { id } = await params;

  return <AffiliateTrendsSection eventId={id} />;
}
