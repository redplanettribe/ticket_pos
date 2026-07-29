import type { ReactNode } from "react";

import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import type { SidebarNavItem } from "@ticket-pos/ui";

import { callBackend } from "@/lib/api";
import type { EventDetail } from "@/lib/events-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { LogoutButton } from "../../logout-button";
import { loadSession } from "../../staff-page-shell";
import { EventHeaderBar } from "./event-header-bar";
import { EventShellClient } from "./event-shell-client";

type EventLayoutProps = {
  params: Promise<{ id: string }>;
  children: ReactNode;
};

async function fetchEvent(eventId: string, token: string): Promise<EventDetail | null> {
  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data;
  } catch {
    return null;
  }
}

// Publish-readiness depends on the persisted Ticket Type count. Fetch it here on
// the server so the header bar reads saved state without any client form state.
async function fetchTicketTypeCount(eventId: string, token: string): Promise<number> {
  try {
    const envelope = await callBackend<unknown[]>(`/api/v1/staff/events/${eventId}/ticket-types`, {
      method: "GET",
      sessionToken: token,
    });
    return Array.isArray(envelope.data) ? envelope.data.length : 0;
  } catch {
    return 0;
  }
}

export default async function EventLayout({ params, children }: EventLayoutProps) {
  const { id } = await params;
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    notFound();
  }

  const [event, ticketTypeCount, session] = await Promise.all([
    fetchEvent(id, token),
    fetchTicketTypeCount(id, token),
    loadSession(),
  ]);

  if (!event) {
    notFound();
  }

  // org_admin / event_owner get full access; event_staff is limited. Tags stay
  // owner-only, but the Sales list is visible to every Member of the Event —
  // Event Staff included — so it appears for all roles.
  const role = session?.active_member?.role;
  const fullAccess = role === "org_admin" || role === "event_owner";
  const navItems: SidebarNavItem[] = [
    { href: `/events/${id}`, label: "Details" },
    { href: `/events/${id}/ticket-types`, label: "Ticket Types" },
    ...(fullAccess ? [{ href: `/events/${id}/tags`, label: "Tags" }] : []),
    { href: `/events/${id}/sales`, label: "Sales" },
  ];

  return (
    <EventShellClient
      eventName={event.name || "Event"}
      status={event.status}
      navItems={navItems}
      userMenu={<LogoutButton />}
    >
      <div className="mx-auto max-w-4xl space-y-6">
        <EventHeaderBar
          eventId={id}
          name={event.name}
          status={event.status}
          slug={event.slug}
          startsAt={event.starts_at}
          timezone={event.timezone}
          ticketTypeCount={ticketTypeCount}
          discoverable={event.discoverable}
        />
        {children}
      </div>
    </EventShellClient>
  );
}
