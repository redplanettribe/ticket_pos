"use client";

import { Menu } from "lucide-react";
import { useState, type ReactNode } from "react";

import { cn } from "../lib/utils";
import { Sheet, SheetContent, SheetTitle } from "./ui/sheet";

export type SidebarNavItem = { href: string; label: string };

/**
 * Content rendered at the top of the sidebar (both the desktop aside and the
 * mobile drawer). May be a render function receiving `onNavigate`, which is
 * defined only for the mobile drawer and closes it — pass it to any interactive
 * element that should dismiss the drawer (e.g. an organization switcher).
 */
export type SidebarHeaderSlot = ReactNode | ((opts: { onNavigate?: () => void }) => ReactNode);

type SidebarShellProps = {
  header: SidebarHeaderSlot;
  /** Product branding shown at the very top of the sidebar (above the header). */
  brand?: ReactNode;
  /** Compact content shown in the mobile top bar beside the menu button. */
  mobileHeader?: ReactNode;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  children: ReactNode;
  /**
   * Copy overrides. Defaults are the English strings, so a caller that passes
   * none renders today's markup byte for byte.
   */
  skipToContentLabel?: string;
  primaryNavLabel?: string;
  navigationMenuTitle?: string;
  openNavigationMenuLabel?: string;
};

type SidebarContentProps = {
  header: SidebarHeaderSlot;
  brand?: ReactNode;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  onNavigate?: () => void;
  primaryNavLabel: string;
};

function SidebarContent({
  header,
  brand,
  navItems,
  activePath,
  userMenu,
  onNavigate,
  primaryNavLabel,
}: SidebarContentProps) {
  return (
    <>
      {brand ? <div className="flex items-center border-b px-4 py-4">{brand}</div> : null}
      {typeof header === "function" ? header({ onNavigate }) : header}
      <nav className="flex flex-1 flex-col gap-1 p-3" aria-label={primaryNavLabel}>
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

export function SidebarShell({
  header,
  brand,
  mobileHeader,
  navItems,
  activePath,
  userMenu,
  children,
  skipToContentLabel = "Skip to main content",
  primaryNavLabel = "Primary",
  navigationMenuTitle = "Navigation menu",
  openNavigationMenuLabel = "Open navigation menu",
}: SidebarShellProps) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  return (
    <div className="flex min-h-screen bg-background">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:shadow"
      >
        {skipToContentLabel}
      </a>
      <aside className="hidden w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <SidebarContent
          header={header}
          brand={brand}
          navItems={navItems}
          activePath={activePath}
          userMenu={userMenu}
          primaryNavLabel={primaryNavLabel}
        />
      </aside>
      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="w-60 p-0">
          <SheetTitle className="sr-only">{navigationMenuTitle}</SheetTitle>
          <SidebarContent
            header={header}
            brand={brand}
            navItems={navItems}
            activePath={activePath}
            userMenu={userMenu}
            primaryNavLabel={primaryNavLabel}
            onNavigate={() => setMobileNavOpen(false)}
          />
        </SheetContent>
      </Sheet>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-3 border-b px-4 py-3 md:hidden">
          <button
            type="button"
            onClick={() => setMobileNavOpen(true)}
            aria-label={openNavigationMenuLabel}
            className="flex size-9 shrink-0 items-center justify-center rounded-md hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <Menu className="size-5" />
          </button>
          {mobileHeader}
        </header>
        <main id="main-content" className="flex-1 p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
