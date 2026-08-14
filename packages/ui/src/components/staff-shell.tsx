"use client";

import type { ReactNode } from "react";

import { staffNavItems, type StaffNavKey } from "../lib/staff-nav";
import { Logo } from "./logo";
import { OrgAvatar } from "./org-avatar";
import { SidebarShell, type SidebarLabels, type SidebarNavItem } from "./sidebar-shell";

/**
 * Every word this shell renders, handed in by the app that knows which language
 * its reader is owed.
 *
 * Required rather than defaulted, unlike `SidebarLabels`: this component has one
 * caller, and a required prop is the only thing that makes a nav entry added to
 * `STAFF_NAV_KEYS` a compile error at the place that has to translate it. A
 * default would have turned that into a Spanish panel with one English row.
 */
export type StaffShellLabels = {
  /** The eyebrow above the Organization's name — "Organization". */
  organizationHeading: string;
  /**
   * The Organization logo's alt text, already naming the Organization.
   *
   * Required for the same reason the rest of this type is: `OrgAvatar` defaults
   * it to an English sentence, and a string read aloud is as much copy as a
   * visible one — it is simply the kind no sighted reviewer of a Spanish page
   * ever catches.
   */
  organizationLogoAlt: string;
  /** One word per `StaffNavKey`, in the reader's language. */
  nav: Record<StaffNavKey, string>;
  sidebar: SidebarLabels;
};

type StaffShellProps = {
  labels: StaffShellLabels;
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
  labels,
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
  // The panel's shape is staffNavItems' decision and its words are the
  // catalog's; this is the one line where the two meet.
  const navItems: SidebarNavItem[] = staffNavItems({
    showEvents,
    showPayouts,
    showSettings,
  }).map((entry) => ({ ...entry, label: labels.nav[entry.key] }));

  const header = ({ onNavigate }: { onNavigate?: () => void }) => {
    const handleOrganizationClick = onOrganizationClick
      ? () => {
          onNavigate?.();
          onOrganizationClick();
        }
      : undefined;

    return (
      <div className="border-b px-4 py-5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {labels.organizationHeading}
        </p>
        {handleOrganizationClick ? (
          <button
            type="button"
            onClick={handleOrganizationClick}
            className="mt-1 flex w-full items-center gap-2 text-left font-semibold hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
          >
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} alt={labels.organizationLogoAlt} shape="inline" />
            <span className="truncate">{organizationName}</span>
            {organizationBadge ? <span className="ml-auto shrink-0">{organizationBadge}</span> : null}
          </button>
        ) : (
          <p className="mt-1 flex items-center gap-2 font-semibold">
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} alt={labels.organizationLogoAlt} shape="inline" />
            <span className="truncate">{organizationName}</span>
            {organizationBadge ? <span className="ml-auto shrink-0">{organizationBadge}</span> : null}
          </p>
        )}
      </div>
    );
  };

  const mobileHeader = (
    <p className="flex min-w-0 items-center gap-2 font-semibold">
      <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} alt={labels.organizationLogoAlt} shape="inline" />
      <span className="truncate">{organizationName}</span>
    </p>
  );

  return (
    <SidebarShell
      header={header}
      labels={labels.sidebar}
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
