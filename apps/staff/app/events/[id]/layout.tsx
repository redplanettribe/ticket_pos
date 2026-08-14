import type { ReactNode } from "react";

import { getTranslations } from "next-intl/server";
import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import type { EventShellLabels } from "@ticket-pos/ui";

import { callBackend } from "@/lib/api";
import { eventStatusKey, type EventDetail } from "@/lib/events-api";
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

  const [event, ticketTypeCount, session, t, shell] = await Promise.all([
    fetchEvent(id, token),
    fetchTicketTypeCount(id, token),
    loadSession(),
    getTranslations("event"),
    getTranslations("shell"),
  ]);

  if (!event) {
    notFound();
  }

  const role = session?.active_member?.role;
  const fullAccess = role === "org_admin" || role === "event_owner";

  /*
    The Event panel's words, resolved here and handed down. @ticket-pos/ui
    cannot reach this catalog — it is shared with the Storefront, whose catalog
    is deliberately a different one (ADR 0041) — so `eventNavItems` returns keys
    and this is the one place that can turn them into a language.
  */
  const labels: EventShellLabels = {
    backToEvents: t("backToEvents"),
    nav: {
      details: t("navDetails"),
      ticketTypes: t("navTicketTypes"),
      affiliateLinks: t("navAffiliateLinks"),
      sales: t("navSales"),
      trends: t("navTrends"),
    },
    sidebar: {
      skipToContent: shell("skipToContent"),
      primaryNavigation: shell("primaryNavigation"),
      navigationMenu: shell("navigationMenu"),
      openNavigationMenu: shell("openNavigationMenu"),
      closeNavigationMenu: shell("closeNavigationMenu"),
    },
  };

  // An unknown status is shown as the API stated it rather than as a blank
  // badge — the same floor an unkeyed error code gets.
  const statusKey = eventStatusKey(event.status);
  const events = await getTranslations("events");
  const statusLabel = statusKey ? events(statusKey) : event.status;

  return (
    <EventShellClient
      labels={labels}
      eventId={id}
      fullAccess={fullAccess}
      eventName={event.name || t("fallbackName")}
      status={event.status}
      statusLabel={statusLabel}
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
          registrationMode={event.registration_mode}
          registrationUrl={event.registration_url}
          registrationClickCount={event.registration_click_count}
          discoverable={event.discoverable}
        />
        {children}
      </div>
    </EventShellClient>
  );
}
