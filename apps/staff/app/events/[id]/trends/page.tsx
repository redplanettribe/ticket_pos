import { permanentRedirect } from "next/navigation";

type LegacyTrendsPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The old address of Sales Trends, kept as a permanent redirect.
 *
 * Trends became the third sub-tab of the Sales surface (#460), and this page is
 * all that is left here: a bookmark, a link in a message, or a browser's
 * autocomplete still lands on the chart. `permanentRedirect` — a 308 — rather
 * than the temporary one the guards use, because this move is not conditional
 * on who is reading; it is where the page lives now, and a client is welcome to
 * remember it.
 *
 * It lives in the route rather than in `next.config`'s `redirects()` so that the
 * fact stays next to the thing it is about: whoever deletes this folder deletes
 * the redirect with it, instead of leaving a rule pointing at nothing.
 *
 * No guard here. The destination carries the Org Admin / Event Owner check and
 * sends an Event Staff member on to the Sales list, so repeating it would put
 * the same rule in two places.
 */
export default async function LegacyEventTrendsPage({ params }: LegacyTrendsPageProps) {
  const { id } = await params;
  permanentRedirect(`/events/${id}/sales/trends`);
}
