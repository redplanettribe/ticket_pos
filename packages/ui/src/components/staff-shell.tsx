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
   * The Payouts entry (#190). Org-Admin-only, the same gate Settings is behind,
   * but a flag of its own so the two can part company later. When a caller says
   * nothing it follows `showSettings`: today one role answers both questions,
   * and a shell that quietly dropped the entry would be the worse failure.
   */
  showPayouts?: boolean;
  showEvents?: boolean;
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
  onOrganizationClick,
}: StaffShellProps) {
  const navItems: SidebarNavItem[] = staffNavItems({
    showEvents,
    showPayouts: showPayouts ?? showSettings,
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
          </button>
        ) : (
          <p className="mt-1 flex items-center gap-2 font-semibold">
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} shape="inline" />
            <span className="truncate">{organizationName}</span>
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
