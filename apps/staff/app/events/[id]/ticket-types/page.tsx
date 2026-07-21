import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { TicketTypesSection } from "../ticket-types-section";

type TicketTypesPageProps = {
  params: Promise<{ id: string }>;
};

type EventSummary = {
  id: string;
  status: string;
};

async function fetchEventStatus(eventId: string): Promise<string | null> {
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
    return envelope.data?.status ?? null;
  } catch {
    return null;
  }
}

export default async function TicketTypesPage({ params }: TicketTypesPageProps) {
  const { id } = await params;
  const status = await fetchEventStatus(id);

  if (status === null) {
    notFound();
  }

  return <TicketTypesSection eventId={id} eventStatus={status} />;
}
