"use client";

import type { ReactNode } from "react";

import { staffNavItems } from "../lib/staff-nav";
import { Logo } from "./logo";
import { OrgAvatar } from "./org-avatar";
import { SidebarShell, type SidebarNavItem } from "./sidebar-shell";

type StaffShellProps = {
  organizationName: string;
  organizationLogoUrl?: string | null;
  children: ReactNode;
  userMenu?: ReactNode;
  activePath?: string;
  showSettings?: boolean;
  /**
   * The Payouts entry (#190). Org-Admin-only, the same gate Settings is behind
   * today, but asked for outright so the two can part company without Payouts
   * silently following the wrong one.
   */
  showPayouts: boolean;
  showEvents?: boolean;
  /**
   * Worn by the organization switcher control — today the count of Payout
   * Requests waiting for a Platform Operator (#191). A node rather than a
   * number: the shell renders what it is handed and does not decide what is
   * worth badging.
   */
  organizationBadge?: ReactNode;
  onOrganizationClick?: () => void;
};

export function StaffShell({
  organizationName,
  organizationLogoUrl,
  children,
  userMenu,
  activePath,
  showSettings = false,
  showPayouts,
  showEvents = false,
  organizationBadge,
  onOrganizationClick,
}: StaffShellProps) {
  const navItems: SidebarNavItem[] = staffNavItems({
    showEvents,
    showPayouts,
    showSettings,
  });

  const header = ({ onNavigate }: { onNavigate?: () => void }) => {
    const handleOrganizationClick = onOrganizationClick
      ? () => {
          onNavigate?.();
          onOrganizationClick();
        }
      : undefined;

    return (
      <div className="border-b px-4 py-5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Organization</p>
        {handleOrganizationClick ? (
          <button
            type="button"
            onClick={handleOrganizationClick}
            className="mt-1 flex w-full items-center gap-2 text-left font-semibold hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
          >
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} shape="inline" />
            <span className="truncate">{organizationName}</span>
            {organizationBadge ? <span className="ml-auto shrink-0">{organizationBadge}</span> : null}
          </button>
        ) : (
          <p className="mt-1 flex items-center gap-2 font-semibold">
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} shape="inline" />
            <span className="truncate">{organizationName}</span>
            {organizationBadge ? <span className="ml-auto shrink-0">{organizationBadge}</span> : null}
          </p>
        )}
      </div>
    );
  };

  const mobileHeader = (
    <p className="flex min-w-0 items-center gap-2 font-semibold">
      <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} shape="inline" />
      <span className="truncate">{organizationName}</span>
    </p>
  );

  return (
    <SidebarShell
      header={header}
      brand={<Logo withWordmark className="text-primary" markClassName="size-6" />}
      mobileHeader={mobileHeader}
      navItems={navItems}
      activePath={activePath}
      userMenu={userMenu}
    >
      {children}
    </SidebarShell>
  );
}
