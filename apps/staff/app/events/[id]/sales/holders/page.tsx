import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { callBackend } from "@/lib/api";
import type { TicketType } from "@/lib/events-api";
import {
  parseHolderDir,
  parseHolderSort,
  type HolderListFilters,
} from "@/lib/holder-list";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../staff-page-shell";
import { HolderListSection, type HolderTicketTypeOption } from "../../holder-list-section";

// The view's whole vocabulary, as it appears in the address bar. One name per
// filter, matching the API's query params so the URL, the fetch and the file
// the Holder Export writes all say the same words (#522, ADR 0065).
type HolderListSearchParams = {
  page?: string;
  outstanding?: string;
  // The structural filters (#523). Named as the API names them, not as the
  // camel-cased fields they parse into: the URL is the shared vocabulary, and
  // a screen whose address said `ticketTypeId` while the request said
  // `ticket_type_id` would be two names for one narrowing.
  ticket_type_id?: string;
  channel?: string;
  sold_from?: string;
  sold_to?: string;
  sort?: string;
  dir?: string;
};

type HolderListPageProps = {
  params: Promise<{ id: string }>;
  searchParams: Promise<HolderListSearchParams>;
};

// parsePage reads the URL page number, flooring at 1 — the same reading the
// Sales list page makes, and the API floors it again regardless.
function parsePage(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "", 10);
  return Number.isFinite(parsed) && parsed >= 1 ? parsed : 1;
}

// parseFilters reads the filters out of the URL, which is the source of truth
// for the view. `outstanding` is on only for the exact value the builder emits:
// a URL is public and half-typed, and anything-but-empty would make
// `?outstanding=no` mean "yes".
//
// THE THREE STRUCTURAL FILTERS ARE TAKEN AS THEY COME (#523), and are NOT
// validated here. A malformed date or an unknown channel is echoed into the
// controls and sent to the API, which ignores what it cannot use and answers
// with the wider roster — the one place that decision is made. Screening it
// here as well would put a second opinion on this side about which filters are
// usable, and the two would disagree the day a fourth Sales Channel exists:
// this page would drop it and the API would honour it.
function parseFilters(searchParams: HolderListSearchParams): HolderListFilters {
  return {
    outstanding: searchParams.outstanding === "true",
    ticketTypeId: searchParams.ticket_type_id ?? "",
    channel: searchParams.channel ?? "",
    soldFrom: searchParams.sold_from ?? "",
    soldTo: searchParams.sold_to ?? "",
  };
}

// fetchTicketTypeOptions loads the Event's Ticket Types to name the ticket-type
// filter's options, tolerating failure so the roster still renders — just with
// that one control offering nothing but "all".
//
// ON THE SERVER, AND NOT IN THE SECTION, exactly as the Sales page loads the
// same list for the same control: the options are part of the page's data, the
// section is a client component that already makes one request per view, and a
// second round trip from the browser would leave the control briefly empty
// while the URL already names a Ticket Type it cannot draw.
async function fetchTicketTypeOptions(
  eventId: string,
  token: string,
): Promise<HolderTicketTypeOption[]> {
  try {
    const envelope = await callBackend<TicketType[]>(
      `/api/v1/staff/events/${eventId}/ticket-types`,
      { method: "GET", sessionToken: token },
    );
    return (envelope.data ?? []).map((type) => ({ id: type.id, name: type.name }));
  } catch {
    return [];
  }
}

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
export default async function SalesHolderListPage({ params, searchParams }: HolderListPageProps) {
  const { id } = await params;
  const resolvedSearchParams = await searchParams;
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
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  const [event, ticketTypes] = await Promise.all([
    loadEvent(id),
    token ? fetchTicketTypeOptions(id, token) : [],
  ]);
  const timezone = event?.timezone ?? null;

  return (
    <HolderListSection
      eventId={id}
      page={parsePage(resolvedSearchParams.page)}
      filters={parseFilters(resolvedSearchParams)}
      ticketTypes={ticketTypes}
      sort={parseHolderSort(resolvedSearchParams.sort)}
      dir={parseHolderDir(resolvedSearchParams.dir)}
      timezone={timezone}
    />
  );
}
