import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import { callBackend } from "@/lib/api";
import type { FeeHandling } from "@/lib/fees";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { TicketTypesSection } from "../ticket-types-section";

type TicketTypesPageProps = {
  params: Promise<{ id: string }>;
};

// What the ticket type form needs from the Event: its lifecycle status, and the
// Fee Handling plus fee schedule behind the derived "Buyers will pay" line.
type EventSummary = {
  id: string;
  status: string;
  fee_handling: FeeHandling;
  fee_basis_points: number;
  fee_iva_basis_points: number;
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
      feeHandling={event.fee_handling}
      feeRates={{
        fee_basis_points: event.fee_basis_points,
        fee_iva_basis_points: event.fee_iva_basis_points,
      }}
    />
  );
}
