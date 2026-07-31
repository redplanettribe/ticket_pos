"use client";

import { Menu } from "lucide-react";
import { useState, type ReactNode } from "react";

import { isNavItemActive } from "../lib/nav-active";
import { cn } from "../lib/utils";
import { Sheet, SheetContent, SheetTitle } from "./ui/sheet";

export type SidebarNavItem = {
  href: string;
  label: string;
  /**
   * True for an entry that must not claim the pages beneath it — the index of a
   * surface, sitting beside its own descendants (see `isNavItemActive`).
   */
  exact?: boolean;
  /**
   * Worn at the end of the entry — today the count of Payout Requests waiting
   * for a Platform Operator (#192). A node rather than a number: the shell
   * renders what it is handed and does not decide what is worth badging.
   */
  badge?: ReactNode;
};

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
};

type SidebarContentProps = {
  header: SidebarHeaderSlot;
  brand?: ReactNode;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  onNavigate?: () => void;
};

function SidebarContent({ header, brand, navItems, activePath, userMenu, onNavigate }: SidebarContentProps) {
  return (
    <>
      {brand ? <div className="flex shrink-0 items-center border-b px-4 py-4">{brand}</div> : null}
      {typeof header === "function" ? header({ onNavigate }) : header}
      {/*
        The nav takes the slack and scrolls on its own, so a nav list longer than
        the panel never pushes the user menu (Sign out) off the foot.
      */}
      <nav className="flex flex-1 flex-col gap-1 overflow-y-auto p-3" aria-label="Primary">
        {navItems.map((item) => {
          const active = isNavItemActive(activePath, item.href, { exact: item.exact });
          return (
            <a
              key={item.href}
              href={item.href}
              onClick={onNavigate}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
                active ? "bg-accent text-accent-foreground" : "text-muted-foreground",
              )}
            >
              <span className="min-w-0 truncate">{item.label}</span>
              {item.badge ? <span className="ml-auto shrink-0">{item.badge}</span> : null}
            </a>
          );
        })}
      </nav>
      {userMenu ? <div className="shrink-0 border-t p-3">{userMenu}</div> : null}
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
}: SidebarShellProps) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  return (
    <div className="flex min-h-screen bg-background">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:shadow"
      >
        Skip to main content
      </a>
      {/*
        The panel holds the viewport while the content beside it scrolls, so Sign
        out is never more than a glance away. Sticky rather than fixed: the
        document stays the scroll container, which keeps `scrollIntoView`,
        document-relative sticky inside the content, and mobile browser-chrome
        auto-hide working.
      */}
      <aside className="sticky top-0 hidden h-dvh w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <SidebarContent
          header={header}
          brand={brand}
          navItems={navItems}
          activePath={activePath}
          userMenu={userMenu}
        />
      </aside>
      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="w-60 p-0">
          <SheetTitle className="sr-only">Navigation menu</SheetTitle>
          <SidebarContent
            header={header}
            brand={brand}
            navItems={navItems}
            activePath={activePath}
            userMenu={userMenu}
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
          {mobileHeader}
        </header>
        <main id="main-content" className="flex-1 p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
