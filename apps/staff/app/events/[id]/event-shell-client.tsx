"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { EventShell, type EventShellLabels } from "@ticket-pos/ui";

import { statusBadgeVariant } from "@/lib/events-api";

type EventShellClientProps = {
  /**
   * The panel's words, resolved on the server by the Event layout. Handed down
   * rather than looked up here for the reason the app shell hands its own down:
   * @ticket-pos/ui is shared with the Storefront and cannot reach the staff
   * catalog (ADR 0041).
   */
  labels: EventShellLabels;
  eventId: string;
  fullAccess: boolean;
  eventName: string;
  /** The API's own token, which still decides the badge's colour. */
  status: string;
  /** The same status in the reader's language, which is what the badge says. */
  statusLabel: string;
  userMenu: ReactNode;
  children: ReactNode;
};

export function EventShellClient({
  labels,
  eventId,
  fullAccess,
  eventName,
  status,
  statusLabel,
  userMenu,
  children,
}: EventShellClientProps) {
  const pathname = usePathname();

  return (
    <EventShell
      labels={labels}
      eventId={eventId}
      fullAccess={fullAccess}
      eventName={eventName}
      statusLabel={statusLabel}
      statusVariant={statusBadgeVariant(status)}
      activePath={pathname}
      userMenu={userMenu}
    >
      {children}
    </EventShell>
  );
}
