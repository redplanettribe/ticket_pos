import { redirect } from "next/navigation";

import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../staff-page-shell";
import { OutstandingAnswersSection } from "../../outstanding-answers-section";

type HolderListPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Holder List tab: the roster, moved whole from `/events/:id/outstanding-answers`
 * (#469). Nothing about the screen changes but its address — it now reads under
 * the Sales layout, so the Net Proceeds strip and the tab strip stand above it
 * as they do over the list and the chart, and the same roster is no longer
 * offered from the Event panel as well.
 */
export default async function SalesHolderListPage({ params }: HolderListPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Org Admins alone, which is the gate the API puts on every Ticket Question
  // and Answer route — narrower than the Event's money surfaces, which also
  // admit an Event Owner. The tab strip hides the tab from everybody else, so
  // this guard is for the URL somebody was sent or bookmarked. A redirect to
  // the list rather than a refusal, the way Record and Trends answer it.
  if (role !== "org_admin") {
    redirect(`/events/${id}/sales`);
  }

  // The feature flags are DELIBERATELY NOT READ HERE. The Sales layout already
  // reads both off the Event payload to decide the tab — the Holder List exists
  // while EITHER is on (#333) — and the API answers 404 to the endpoint while
  // both are dark regardless (ADR 0045). A second copy of that decision on this
  // page could only ever disagree with one of them, and somebody who reached
  // this URL with both features off sees the surface's load failure — which is
  // the correct amount of information: none.
  //
  // The Event's timezone, so "sold in January" is January where the Event is and
  // not where the reader is standing. Tolerates failure: without it the list still
  // renders and the dates fall back to the viewer's own zone, which is a worse
  // answer than the right one and a much better answer than no list.
  const event = await loadEvent(id);
  const timezone = event?.timezone ?? null;

  return <OutstandingAnswersSection eventId={id} timezone={timezone} />;
}
