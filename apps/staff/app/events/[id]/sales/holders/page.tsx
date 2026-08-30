import { redirect } from "next/navigation";

import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../staff-page-shell";
import { HolderListSection } from "../../holder-list-section";

type HolderListPageProps = {
  params: Promise<{ id: string }>;
};

/**
 * The Holder List tab: the roster, moved whole from `/events/:id/outstanding-answers`
 * (#469). Nothing about the screen changes but its address — it now reads under
 * the Sales layout, so the Net Proceeds strip and the tab strip stand above it
 * as they do over the list and the chart, and the same roster is no longer
 * offered from the Event panel as well.
 *
 * THE OLD ADDRESS IS GONE ENTIRELY (#519). The 308 that stood there for a
 * release is deleted with the rest of the surface's first name: this is the
 * only page of the Holder List, and a bookmark from before #469 now 404s rather
 * than redirecting. A stale staff bookmark is one person clicking the tab
 * again, and the ADR 0065 rename's compatibility budget is spent on the API
 * alias, where the two deploys really can disagree.
 */
export default async function SalesHolderListPage({ params }: HolderListPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // The Org Admin and the Event Owner, which is the gate the API now puts on
  // the read (#521, ADR 0065) and the one the Event's money surfaces have
  // always carried. It used to be Org Admins alone, inherited from the Ticket
  // Question routes the list grew out of, while the Sales Export next door
  // already handed an Event Owner the same Holders' names and addresses in a
  // file — a narrower gate on the screen than on the download of it.
  //
  // EVENT STAFF ARE STILL SENT AWAY. They are Members of the Event and reach
  // the Sales list beneath this redirect; the roster is not theirs, and if this
  // condition ever becomes a truthiness check on `role` that is what breaks.
  //
  // The tab strip hides the tab from everybody it is not for, so this guard is
  // only ever met by a URL somebody was sent or bookmarked — which is why it
  // REDIRECTS to the list rather than refusing, the way Record and Trends
  // answer the same URL. The API refuses independently; nothing here is the
  // security boundary.
  if (role !== "org_admin" && role !== "event_owner") {
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

  return <HolderListSection eventId={id} timezone={timezone} />;
}
