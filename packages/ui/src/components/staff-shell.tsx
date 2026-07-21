"use client";

import type { ReactNode } from "react";

import { OrgAvatar } from "./org-avatar";
import { SidebarShell, type SidebarNavItem } from "./sidebar-shell";

type StaffShellProps = {
  organizationName: string;
  organizationLogoUrl?: string | null;
  children: ReactNode;
  userMenu?: ReactNode;
  activePath?: string;
  showSettings?: boolean;
  showEvents?: boolean;
  onOrganizationClick?: () => void;
};

const baseNavItems = [
  { href: "/", label: "Dashboard" },
  { href: "/pos", label: "POS" },
  { href: "/imports", label: "Imports" },
] as const;

export function StaffShell({
  organizationName,
  organizationLogoUrl,
  children,
  userMenu,
  activePath,
  showSettings = false,
  showEvents = false,
  onOrganizationClick,
}: StaffShellProps) {
  const navItems: SidebarNavItem[] = [
    ...baseNavItems.slice(0, 1),
    ...(showEvents ? [{ href: "/events", label: "Events" }] : []),
    ...baseNavItems.slice(1),
    ...(showSettings ? [{ href: "/settings", label: "Settings" }] : []),
  ];

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
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
            <span className="truncate">{organizationName}</span>
          </button>
        ) : (
          <p className="mt-1 flex items-center gap-2 font-semibold">
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
            <span className="truncate">{organizationName}</span>
          </p>
        )}
      </div>
    );
  };

  const mobileHeader = (
    <p className="flex min-w-0 items-center gap-2 font-semibold">
      <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
      <span className="truncate">{organizationName}</span>
    </p>
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
