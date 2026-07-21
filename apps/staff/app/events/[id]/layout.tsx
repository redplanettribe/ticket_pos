import type { ReactNode } from "react";

import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import type { SidebarNavItem } from "@ticket-pos/ui";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { LogoutButton } from "../../logout-button";
import { loadSession } from "../../staff-page-shell";
import { EventShellClient } from "./event-shell-client";

type EventLayoutProps = {
  params: Promise<{ id: string }>;
  children: ReactNode;
};

type EventSummary = {
  id: string;
  name: string;
  status: string;
};

async function fetchEvent(eventId: string): Promise<EventSummary | null> {
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
    return envelope.data;
  } catch {
    return null;
  }
}

export default async function EventLayout({ params, children }: EventLayoutProps) {
  const { id } = await params;
  const [event, session] = await Promise.all([fetchEvent(id), loadSession()]);

  if (!event) {
    notFound();
  }

  // org_admin / event_owner get full access; event_staff is limited. Tags and
  // Sales are org_admin / event_owner only, so they only appear for those roles.
  const role = session?.active_member?.role;
  const fullAccess = role === "org_admin" || role === "event_owner";
  const navItems: SidebarNavItem[] = [
    { href: `/events/${id}`, label: "Details" },
    { href: `/events/${id}/ticket-types`, label: "Ticket Types" },
    ...(fullAccess
      ? [
          { href: `/events/${id}/tags`, label: "Tags" },
          { href: `/events/${id}/sales`, label: "Sales" },
        ]
      : []),
  ];

  return (
    <EventShellClient
      eventName={event.name || "Event"}
      status={event.status}
      navItems={navItems}
      userMenu={<LogoutButton />}
    >
      <div className="mx-auto max-w-4xl">{children}</div>
    </EventShellClient>
  );
}
