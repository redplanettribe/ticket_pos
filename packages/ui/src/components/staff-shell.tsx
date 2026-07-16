"use client";

import { Menu } from "lucide-react";
import { useState, type ReactNode } from "react";

import { cn } from "../lib/utils";
import { OrgAvatar } from "./org-avatar";
import { Sheet, SheetContent, SheetTitle } from "./ui/sheet";

type NavItem = { href: string; label: string };

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

type SidebarContentProps = {
  organizationName: string;
  organizationLogoUrl?: string | null;
  navItems: NavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  onOrganizationClick?: () => void;
  onNavigate?: () => void;
};

function SidebarContent({
  organizationName,
  organizationLogoUrl,
  navItems,
  activePath,
  userMenu,
  onOrganizationClick,
  onNavigate,
}: SidebarContentProps) {
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
    <>
      <div className="border-b px-4 py-5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Organization</p>
        {organizationNameElement}
      </div>
      <nav className="flex flex-1 flex-col gap-1 p-3" aria-label="Primary">
        {navItems.map((item) => (
          <a
            key={item.href}
            href={item.href}
            onClick={onNavigate}
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
    </>
  );
}

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
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  const navItems: NavItem[] = [
    ...baseNavItems.slice(0, 1),
    ...(showEvents ? [{ href: "/events", label: "Events" }] : []),
    ...baseNavItems.slice(1),
    ...(showSettings ? [{ href: "/settings", label: "Settings" }] : []),
  ];

  function handleDrawerOrganizationClick() {
    setMobileNavOpen(false);
    onOrganizationClick?.();
  }

  return (
    <div className="flex min-h-screen bg-background">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:shadow"
      >
        Skip to main content
      </a>
      <aside className="hidden w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <SidebarContent
          organizationName={organizationName}
          organizationLogoUrl={organizationLogoUrl}
          navItems={navItems}
          activePath={activePath}
          userMenu={userMenu}
          onOrganizationClick={onOrganizationClick}
        />
      </aside>
      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="w-60 p-0">
          <SheetTitle className="sr-only">Navigation menu</SheetTitle>
          <SidebarContent
            organizationName={organizationName}
            organizationLogoUrl={organizationLogoUrl}
            navItems={navItems}
            activePath={activePath}
            userMenu={userMenu}
            onOrganizationClick={onOrganizationClick ? handleDrawerOrganizationClick : undefined}
            onNavigate={() => setMobileNavOpen(false)}
          />
        </SheetContent>
      </Sheet>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-3 border-b px-4 py-3 md:hidden">
          <button
            type="button"
            onClick={() => setMobileNavOpen(true)}
            aria-label="Open navigation menu"
            className="flex size-9 shrink-0 items-center justify-center rounded-md hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <Menu className="size-5" />
          </button>
          <p className="flex min-w-0 items-center gap-2 font-semibold">
            <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
            <span className="truncate">{organizationName}</span>
          </p>
        </header>
        <main id="main-content" className="flex-1 p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
