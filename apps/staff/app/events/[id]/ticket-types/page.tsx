import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import { callBackend } from "@/lib/api";
import type { FeeHandling } from "@/lib/fees";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { TicketTypesSection } from "../ticket-types-section";

type TicketTypesPageProps = {
  params: Promise<{ id: string }>;
};

// What the ticket type form needs from the Event: its lifecycle status, the
// Fee Handling plus fee schedule behind the derived "Buyers will pay" line, and
// the timezone Promotion windows are typed and read in (ADR 0021).
type EventSummary = {
  id: string;
  status: string;
  starts_at: string | null;
  timezone: string | null;
  fee_handling: FeeHandling;
  fee_basis_points: number;
  fee_iva_basis_points: number;
  /**
   * The platform's Ticket Question feature flag, which rides on the Event
   * payload for the same reason the fee rates above do: this page composes the
   * Ticket Type editor, and the flag decides whether that editor offers a Ticket
   * Question surface at all (#309, ADR 0045). Reading it from the API rather
   * than from an environment variable of the frontend's own keeps one answer to
   * the question of whether the feature is on.
   */
  ticket_questions_enabled: boolean;
};

async function fetchEventSummary(eventId: string): Promise<EventSummary | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return null;
  }
  try {
    const envelope = await callBackend<EventSummary>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data ?? null;
  } catch {
    return null;
  }
}

export default async function TicketTypesPage({ params }: TicketTypesPageProps) {
  const { id } = await params;
  const event = await fetchEventSummary(id);

  if (event === null) {
    notFound();
  }

  return (
    <TicketTypesSection
      eventId={id}
      eventStatus={event.status}
      eventTimezone={event.timezone}
      eventStartsAt={event.starts_at}
      ticketQuestionsEnabled={event.ticket_questions_enabled}
      feeHandling={event.fee_handling}
      feeRates={{
        fee_basis_points: event.fee_basis_points,
        fee_iva_basis_points: event.fee_iva_basis_points,
      }}
    />
  );
}
