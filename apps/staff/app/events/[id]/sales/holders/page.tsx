import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { callBackend } from "@/lib/api";
import type { TicketType } from "@/lib/events-api";
import type { TicketQuestion } from "@/lib/ticket-questions";
import {
  parseHolderDir,
  parseHolderSort,
  type HolderListFilters,
} from "@/lib/holder-list";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../staff-page-shell";
import {
  HolderListSection,
  type HolderQuestionOption,
  type HolderTicketTypeOption,
} from "../../holder-list-section";

// The view's whole vocabulary, as it appears in the address bar. One name per
// filter, matching the API's query params so the URL, the fetch and the file
// the Holder Export writes all say the same words (#522, ADR 0065).
type HolderListSearchParams = {
  page?: string;
  // The search term (#526), named `q` as the API and the Sales list name it.
  // A customer's address can be in here, and therefore in browser history and
  // in any pasted link — accepted only because the Sales list already does
  // exactly this (ADR 0065). What it may MATCH is a stricter question the API
  // answers: searchable if and only if displayable, so an unaccepted Holder's
  // address is not findable through this parameter.
  q?: string;
  outstanding?: string;
  // One named Ticket Question's debtors (#525). The question's ID and not its
  // words: a label is the Organization's own wording and may be corrected, and
  // a URL keyed on it would stop meaning anything the day it was.
  question_id?: string;
  // Where a Ticket stands with its Holder (#524). Four values over three
  // states: `never_accepted` is a value of this filter and not a fourth state,
  // which is `HolderStateValue`'s whole subject.
  assignment_state?: string;
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
// THE OTHER FILTERS ARE TAKEN AS THEY COME (#523, #524), and are NOT validated
// here. A malformed date, an unknown channel or an unknown assignment state is
// echoed into the controls and sent to the API, which ignores what it cannot
// use and answers with the wider roster — the one place that decision is made.
// Screening it here as well would put a second opinion on this side about which
// filters are usable, and the two would disagree the day a fourth Sales Channel
// exists: this page would drop it and the API would honour it.
//
// AND `assignment_state` IS PASSED THROUGH EVEN WHERE TICKET ASSIGNMENT IS
// DARK, on the same argument one rung further. The API drops it and returns the
// whole roster, so a bookmark carrying it still works; this page reads no flag
// to second-guess that, and the CONTROL's existence is decided from the
// payload's absences in the section below, never from a flag passed down.
function parseFilters(searchParams: HolderListSearchParams): HolderListFilters {
  return {
    outstanding: searchParams.outstanding === "true",
    // Taken as it comes, like every other filter here: a search term is free
    // text with no malformed value to screen for, and the API binds it as a
    // query argument with its LIKE metacharacters escaped.
    q: searchParams.q ?? "",
    questionId: searchParams.question_id ?? "",
    assignmentState: searchParams.assignment_state ?? "",
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

// fetchQuestionOptions loads the Event's Ticket Questions to name the
// named-question filter's options (#525), tolerating failure exactly as
// `fetchTicketTypeOptions` above does — the control then offers only "any
// question", and the roster is unaffected.
//
// AN EVENT'S QUESTION SET IS THE UNION ACROSS ITS TICKET TYPES, because a
// Ticket Question belongs to a TICKET TYPE and never to the Event (a Ticket of
// the General type owes nothing the VIP type asks). The only staff read of them
// is per-Ticket-Type — `/ticket-types/:id/questions`, the one the Ticket
// Questions editor uses — so this costs ONE CALL PER TICKET TYPE, issued in
// parallel and on the server, where they are cheap and invisible.
//
// THAT COST WAS WEIGHED AGAINST ADDING AN EVENT-WIDE STAFF ENDPOINT, and the
// N calls won for now: an Event has a handful of Ticket Types, the reads are
// already warm from `fetchTicketTypeOptions`, and a new route would be a second
// way to read the same rows — one more surface to gate, to document and to keep
// agreeing with the editor's. If an Event ever carries enough Ticket Types for
// this to show, the fix is that endpoint and not a cache here.
//
// EACH CALL FAILS ALONE. One Ticket Type's read erroring leaves the others'
// questions on the control rather than emptying it: a partial list of questions
// is a working filter, and an empty one is a control that cannot be used.
//
// THE QUESTIONS ARE OFFERED AS THEY COME — every one of them, live or retired,
// required or optional. It is tempting to drop the ones nobody can owe, and it
// is exactly the mistake this ticket exists to avoid: `required`, `retired` and
// `approved` are three of the four clauses that DEFINE an Outstanding Answer,
// that definition lives once in the API, and a copy of half of it here would be
// the copy that drifts. The cost is a reader who picks a retired question and
// gets an empty roster — which is the truthful answer, since nobody owes it.
async function fetchQuestionOptions(
  eventId: string,
  token: string,
  ticketTypes: readonly HolderTicketTypeOption[],
): Promise<HolderQuestionOption[]> {
  const perType = await Promise.all(
    ticketTypes.map(async (type) => {
      try {
        const envelope = await callBackend<TicketQuestion[]>(
          `/api/v1/staff/events/${eventId}/ticket-types/${type.id}/questions`,
          { method: "GET", sessionToken: token },
        );
        return envelope.data ?? [];
      } catch {
        return [];
      }
    }),
  );

  // Flattened in the Ticket Types' own order, each type's questions in the
  // order that type asks them — "the Event's questions in their own order",
  // which is the order the form reads in and therefore the order an Organizer
  // already has in their head.
  //
  // DEDUPED ON THE ID and never on the label. Two Ticket Types may ask
  // identically-worded questions, and those are two different questions owed by
  // different Tickets; collapsing them would hide one of the two behind a
  // heading that answers for the other. A repeated heading costs a reader
  // nothing here, because the option's VALUE is the id.
  const seen = new Set<string>();
  const options: HolderQuestionOption[] = [];
  for (const question of perType.flat()) {
    if (seen.has(question.id)) continue;
    seen.add(question.id);
    options.push({ id: question.id, label: question.label });
  }
  return options;
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
  const canExport = role === "org_admin" || role === "event_owner";
  if (!canExport) {
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
  // AFTER the Ticket Types and not beside them: an Event's question set is the
  // union across its Ticket Types, so this read cannot start until it knows
  // which types there are. With Ticket Questions dark every one of these calls
  // 404s and the list comes back empty — which is right, and is not what
  // decides whether the control is drawn: that is read off the payload's
  // absences in the section, never from a flag or from an empty options list
  // (ADR 0045, and #524's comment on why no flag prop comes down here).
  const questions = token ? await fetchQuestionOptions(id, token, ticketTypes) : [];
  const timezone = event?.timezone ?? null;

  return (
    <HolderListSection
      eventId={id}
      page={parsePage(resolvedSearchParams.page)}
      filters={parseFilters(resolvedSearchParams)}
      ticketTypes={ticketTypes}
      questions={questions}
      sort={parseHolderSort(resolvedSearchParams.sort)}
      dir={parseHolderDir(resolvedSearchParams.dir)}
      timezone={timezone}
      /*
        WHO MAY DOWNLOAD THE HOLDER EXPORT (#529), computed from the role this
        page ALREADY read for its redirect above rather than read again in the
        client component. One reading of who this person is, used twice: a second
        one could disagree with the guard it stands behind, and the disagreement
        would show as a button that 403s. Today it is `true` on every render that
        gets this far — the redirect saw to that — and it is passed explicitly
        anyway, so that widening either gate is a visible edit rather than a
        silent consequence of the other.
      */
      canExport={canExport}
    />
  );
}
