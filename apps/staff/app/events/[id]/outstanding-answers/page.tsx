import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { callBackend } from "@/lib/api";
import type { EventDetail } from "@/lib/events-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { loadSession } from "../../../staff-page-shell";
import { OutstandingAnswersSection } from "../outstanding-answers-section";

type OutstandingAnswersPageProps = {
  params: Promise<{ id: string }>;
};

// The Event's timezone, so "sold in January" is January where the Event is and
// not where the reader is standing. Tolerates failure: without it the list still
// renders and the dates fall back to the viewer's own zone, which is a worse
// answer than the right one and a much better answer than no list.
async function fetchEventTimezone(eventId: string, token: string): Promise<string | null> {
  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data?.timezone ?? null;
  } catch {
    return null;
  }
}

export default async function EventOutstandingAnswersPage({ params }: OutstandingAnswersPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // Org Admins alone, which is the gate the API puts on every Ticket Question
  // and Answer route — narrower than the Event's money surfaces, which also
  // admit an Event Owner. The nav hides the tab from everybody else, so this
  // guard is for the URL somebody was sent or bookmarked.
  if (role !== "org_admin") {
    redirect(`/events/${id}/sales`);
  }

  // The feature flag is DELIBERATELY NOT READ HERE. The layout already reads it
  // off the Event payload to decide the nav entry, and the API answers 404 to
  // the endpoint while the feature is dark regardless (ADR 0045). A second copy
  // of that decision on this page could only ever disagree with one of them, and
  // somebody who reached this URL with the feature off sees the surface's load
  // failure — which is the correct amount of information: none.
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  const timezone = token ? await fetchEventTimezone(id, token) : null;

  return <OutstandingAnswersSection eventId={id} timezone={timezone} />;
}
