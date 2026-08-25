import { notFound } from "next/navigation";

import { loadEvent } from "@/lib/staff-event";

import { TicketTypesSection } from "../ticket-types-section";

type TicketTypesPageProps = {
  params: Promise<{ id: string }>;
};

// What the ticket type form needs from the Event: its lifecycle status, the
// Fee Handling plus fee schedule behind the derived "Buyers will pay" line, the
// timezone Promotion windows are typed and read in (ADR 0021), and the Ticket
// Question feature flag that decides whether the editor offers a Ticket
// Question surface at all (#309, ADR 0045). All of it rides on the Event
// payload the layout already reads, shared through `loadEvent` (#446).

export default async function TicketTypesPage({ params }: TicketTypesPageProps) {
  const { id } = await params;
  const event = await loadEvent(id);

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
