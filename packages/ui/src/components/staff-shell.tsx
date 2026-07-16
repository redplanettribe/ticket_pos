"use client";

import type { ReactNode } from "react";

import { cn } from "../lib/utils";
import { OrgAvatar } from "./org-avatar";

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
  const navItems = [
    ...baseNavItems.slice(0, 1),
    ...(showEvents ? [{ href: "/events", label: "Events" as const }] : []),
    ...baseNavItems.slice(1),
    ...(showSettings ? [{ href: "/settings", label: "Settings" as const }] : []),
  ];

  const organizationNameElement = onOrganizationClick ? (
    <button
      type="button"
      onClick={onOrganizationClick}
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
  );

  return (
    <div className="flex min-h-screen bg-background">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:shadow"
      >
        Skip to main content
      </a>
      <aside className="hidden w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <div className="border-b px-4 py-5">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Organization</p>
          {organizationNameElement}
        </div>
        <nav className="flex flex-1 flex-col gap-1 p-3" aria-label="Primary">
          {navItems.map((item) => (
            <a
              key={item.href}
              href={item.href}
              aria-current={activePath === item.href ? "page" : undefined}
              className={cn(
                "rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
                activePath === item.href ? "bg-accent text-accent-foreground" : "text-muted-foreground",
              )}
            >
              {item.label}
            </a>
          ))}
        </nav>
        {userMenu ? <div className="border-t p-3">{userMenu}</div> : null}
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center border-b px-4 py-3 md:hidden">
          {onOrganizationClick ? (
            <button
              type="button"
              onClick={onOrganizationClick}
              className="flex items-center gap-2 font-semibold hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
            >
              <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
              <span className="truncate">{organizationName}</span>
            </button>
          ) : (
            <p className="flex items-center gap-2 font-semibold">
              <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
              <span className="truncate">{organizationName}</span>
            </p>
          )}
        </header>
        <main id="main-content" className="flex-1 p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
