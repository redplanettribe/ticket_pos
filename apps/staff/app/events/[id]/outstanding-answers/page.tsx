import { permanentRedirect } from "next/navigation";

type LegacyHolderListPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The old address of the Holder List, kept as a permanent redirect.
 *
 * The Holder List became the last sub-tab of the Sales surface (#469), and this
 * page is all that is left here: a bookmark, a link in a message, or a browser's
 * autocomplete still lands on the roster. The path carried the screen's first
 * name, Outstanding Answers (#313), long after the screen was widened to the
 * roster (#333); the move was the moment to stop carrying it. `permanentRedirect`
 * — a 308 — rather than the temporary one the guards use, because this move is
 * not conditional on who is reading; it is where the page lives now, and a
 * client is welcome to remember it.
 *
 * It lives in the route rather than in `next.config`'s `redirects()` so that the
 * fact stays next to the thing it is about: whoever deletes this folder deletes
 * the redirect with it, instead of leaving a rule pointing at nothing.
 *
 * No guard here. The destination carries the Org Admin check and sends anybody
 * else on to the Sales list, so repeating it would put the same rule in two
 * places.
 */
export default async function LegacyOutstandingAnswersPage({ params }: LegacyHolderListPageProps) {
  const { id } = await params;
  permanentRedirect(`/events/${id}/sales/holders`);
}
