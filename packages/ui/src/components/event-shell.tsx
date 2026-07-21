"use client";

import { ArrowLeft } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { Badge } from "./ui/badge";
import { SidebarShell, type SidebarNavItem } from "./sidebar-shell";

type BadgeVariant = ComponentProps<typeof Badge>["variant"];

type EventShellProps = {
  eventName: string;
  status: string;
  statusVariant?: BadgeVariant;
  /** Where the back affordance links to. Defaults to the Events list. */
  backHref?: string;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  children: ReactNode;
};

/**
 * Event-scoped sidebar shell. Replaces the org `StaffShell` when a Member opens
 * an Event: a back-to-Events affordance plus the Event name and status badge in
 * the header, with role-gated nav items supplied by the app.
 */
export function EventShell({
  eventName,
  status,
  statusVariant,
  backHref = "/events",
  navItems,
  activePath,
  userMenu,
  children,
}: EventShellProps) {
  const header = ({ onNavigate }: { onNavigate?: () => void }) => (
    <div className="border-b px-4 py-5">
      <a
        href={backHref}
        onClick={onNavigate}
        className="flex items-center gap-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
      >
        <ArrowLeft className="size-4" />
        Events
      </a>
      <div className="mt-3 flex items-center gap-2">
        <span className="min-w-0 truncate font-semibold">{eventName}</span>
        <Badge variant={statusVariant} className="shrink-0 capitalize">
          {status}
        </Badge>
      </div>
    </div>
  );

  const mobileHeader = (
    <div className="flex min-w-0 items-center gap-2">
      <span className="min-w-0 truncate font-semibold">{eventName}</span>
      <Badge variant={statusVariant} className="shrink-0 capitalize">
        {status}
      </Badge>
    </div>
  );

  return (
    <SidebarShell
      header={header}
      mobileHeader={mobileHeader}
      navItems={navItems}
      activePath={activePath}
      userMenu={userMenu}
    >
      {children}
    </SidebarShell>
  );
}
