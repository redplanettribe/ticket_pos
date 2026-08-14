"use client";

import type { ReactNode } from "react";

import { operatorNavItems, type OperatorNavKey } from "../lib/operator-nav";
import { Logo } from "./logo";
import { PlatformMark } from "./platform-mark";
import { SidebarShell, type SidebarLabels, type SidebarNavItem } from "./sidebar-shell";

/** Every word the Operator Dashboard's panel renders. See `StaffShellLabels`. */
export type OperatorShellLabels = {
  /** The eyebrow above the Platform entry — "Operator". */
  operatorHeading: string;
  /** What the surface is called where an Organization's name would be. */
  platform: string;
  /** One word per `OperatorNavKey`, in the reader's language. */
  nav: Record<OperatorNavKey, string>;
  sidebar: SidebarLabels;
};

type OperatorShellProps = {
  labels: OperatorShellLabels;
  children: ReactNode;
  userMenu?: ReactNode;
  activePath?: string;
  /**
   * Worn by the Payout Requests entry — how many are waiting to be answered, or
   * nothing (#192). The count an operator saw on the switcher before crossing
   * over is still in front of them afterwards.
   */
  payoutRequestBadge?: ReactNode;
  /** Opens the organization switcher, the way back to any Organization. */
  onPlatformClick?: () => void;
};

/**
 * The Operator Dashboard's sidebar shell. Replaces the org `StaffShell`
 * throughout the operator surface, the same takeover `EventShell` performs
 * inside an Event.
 *
 * A Platform Operator is not acting as any Organization (CONTEXT.md), so no
 * Organization's name, logo or navigation appears here — the header reads
 * Platform and opens the switcher they crossed over with, which is also the way
 * back to an Organization they belong to.
 */
export function OperatorShell({
  labels,
  children,
  userMenu,
  activePath,
  payoutRequestBadge,
  onPlatformClick,
}: OperatorShellProps) {
  const navItems: SidebarNavItem[] = operatorNavItems({ payoutRequestBadge }).map((entry) => ({
    ...entry,
    label: labels.nav[entry.key],
  }));

  const header = ({ onNavigate }: { onNavigate?: () => void }) => {
    const handlePlatformClick = onPlatformClick
      ? () => {
          onNavigate?.();
          onPlatformClick();
        }
      : undefined;

    return (
      <div className="border-b px-4 py-5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {labels.operatorHeading}
        </p>
        {handlePlatformClick ? (
          <button
            type="button"
            onClick={handlePlatformClick}
            className="mt-1 flex w-full items-center gap-2 text-left font-semibold hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
          >
            <PlatformMark shape="inline" />
            <span className="truncate">{labels.platform}</span>
          </button>
        ) : (
          <p className="mt-1 flex items-center gap-2 font-semibold">
            <PlatformMark shape="inline" />
            <span className="truncate">{labels.platform}</span>
          </p>
        )}
      </div>
    );
  };

  const mobileHeader = (
    <p className="flex min-w-0 items-center gap-2 font-semibold">
      <PlatformMark shape="inline" />
      <span className="truncate">{labels.platform}</span>
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
