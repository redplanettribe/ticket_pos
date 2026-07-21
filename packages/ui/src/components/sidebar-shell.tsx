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
  /** Compact content shown in the mobile top bar beside the menu button. */
  mobileHeader?: ReactNode;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  children: ReactNode;
};

type SidebarContentProps = {
  header: SidebarHeaderSlot;
  navItems: SidebarNavItem[];
  activePath?: string;
  userMenu?: ReactNode;
  onNavigate?: () => void;
};

function SidebarContent({ header, navItems, activePath, userMenu, onNavigate }: SidebarContentProps) {
  return (
    <>
      {typeof header === "function" ? header({ onNavigate }) : header}
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

export function SidebarShell({
  header,
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
      <aside className="hidden w-60 shrink-0 border-r bg-card md:flex md:flex-col">
        <SidebarContent header={header} navItems={navItems} activePath={activePath} userMenu={userMenu} />
      </aside>
      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="w-60 p-0">
          <SheetTitle className="sr-only">Navigation menu</SheetTitle>
          <SidebarContent
            header={header}
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
