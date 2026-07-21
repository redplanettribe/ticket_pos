"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { EventShell, type SidebarNavItem } from "@ticket-pos/ui";

import { statusBadgeVariant } from "@/lib/events-api";

type EventShellClientProps = {
  eventName: string;
  status: string;
  navItems: SidebarNavItem[];
  userMenu: ReactNode;
  children: ReactNode;
};

export function EventShellClient({ eventName, status, navItems, userMenu, children }: EventShellClientProps) {
  const pathname = usePathname();

  return (
    <EventShell
      eventName={eventName}
      status={status}
      statusVariant={statusBadgeVariant(status)}
      navItems={navItems}
      activePath={pathname}
      userMenu={userMenu}
    >
      {children}
    </EventShell>
  );
}
