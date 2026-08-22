"use client";

import { ArrowLeft } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { eventNavItems, type EventNavKey } from "../lib/event-nav";
import { Badge } from "./ui/badge";
import { SidebarShell, type SidebarLabels, type SidebarNavItem } from "./sidebar-shell";

type BadgeVariant = ComponentProps<typeof Badge>["variant"];

/**
 * Every word this shell renders, handed in by the app that knows which language
 * its reader is owed.
 *
 * Required rather than defaulted, for the same reason `StaffShellLabels` is: an
 * entry added to `EVENT_NAV_KEYS` has to become a compile error at the one place
 * that can translate it, and a default would have turned that into a Spanish
 * panel with one English row.
 */
export type EventShellLabels = {
  /** The back affordance above the Event's name — "Events". */
  backToEvents: string;
  /** One phrase per `EventNavKey`, in the reader's language. */
  nav: Record<EventNavKey, string>;
  sidebar: SidebarLabels;
};

type EventShellProps = {
  labels: EventShellLabels;
  eventName: string;
  /**
   * The Event's status in words — already translated, because "draft" is a token
   * the API states and not something to render at a reader (ADR 0041).
   */
  statusLabel: string;
  statusVariant?: BadgeVariant;
  /** Where the back affordance links to. Defaults to the Events list. */
  backHref?: string;
  /** Whether this Member sees the owner-only entries. */
  fullAccess: boolean;
  /**
   * Whether this Member sees the Event's Holder List entry: Ticket Assignment
   * or Ticket Questions is on AND they are an Org Admin. Separate from
   * `fullAccess` because it is a narrower gate over features that ship dark —
   * see `eventNavItems`.
   */
  holderList?: boolean;
  eventId: string;
  activePath?: string;
  userMenu?: ReactNode;
  children: ReactNode;
};

/**
 * Event-scoped sidebar shell. Replaces the org `StaffShell` when a Member opens
 * an Event: a back-to-Events affordance plus the Event name and status badge in
 * the header, with role-gated nav items derived from `eventNavItems`.
 */
export function EventShell({
  labels,
  eventName,
  statusLabel,
  statusVariant,
  backHref = "/events",
  fullAccess,
  holderList = false,
  eventId,
  activePath,
  userMenu,
  children,
}: EventShellProps) {
  // The panel's shape is eventNavItems' decision and its words are the
  // catalog's; this is the one line where the two meet.
  const navItems: SidebarNavItem[] = eventNavItems({
    eventId,
    fullAccess,
    holderList,
  }).map((entry) => ({
    ...entry,
    label: labels.nav[entry.key],
  }));

  const header = ({ onNavigate }: { onNavigate?: () => void }) => (
    <div className="border-b px-4 py-5">
      <a
        href={backHref}
        onClick={onNavigate}
        className="flex items-center gap-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
      >
        <ArrowLeft className="size-4" />
        {labels.backToEvents}
      </a>
      <div className="mt-3 flex items-center gap-2">
        <span className="min-w-0 truncate font-semibold">{eventName}</span>
        <Badge variant={statusVariant} className="shrink-0">
          {statusLabel}
        </Badge>
      </div>
    </div>
  );

  const mobileHeader = (
    <div className="flex min-w-0 items-center gap-2">
      <span className="min-w-0 truncate font-semibold">{eventName}</span>
      <Badge variant={statusVariant} className="shrink-0">
        {statusLabel}
      </Badge>
    </div>
  );

  return (
    <SidebarShell
      header={header}
      labels={labels.sidebar}
      mobileHeader={mobileHeader}
      navItems={navItems}
      activePath={activePath}
      userMenu={userMenu}
    >
      {children}
    </SidebarShell>
  );
}
