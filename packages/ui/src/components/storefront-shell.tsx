import type { ReactNode } from "react";

import { LogoMark } from "./logo";
import { OrgAvatar } from "./org-avatar";

type StorefrontShellProps = {
  children: ReactNode;
  organizationName?: string;
  organizationLogoUrl?: string | null;
};

export function StorefrontShell({ children, organizationName, organizationLogoUrl }: StorefrontShellProps) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-6xl items-center gap-2 px-4 py-2">
          {organizationName ? (
            <>
              <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} />
              <p className="text-sm font-medium text-muted-foreground">{organizationName}</p>
            </>
          ) : (
            // Global explorer: just the Multiticketing mark (no wordmark),
            // linked home. The organizer owns the visible name on org pages.
            <a href="/" aria-label="Multiticketing" className="inline-flex">
              <LogoMark aria-hidden className="size-8 text-primary" />
            </a>
          )}
        </div>
      </header>
      <div className="flex-1">{children}</div>
      <footer className="border-t py-6">
        <div className="mx-auto w-full max-w-6xl px-4 text-center text-sm text-muted-foreground">
          <a href="https://multiticketing.com" className="hover:text-foreground">
            Powered by Multiticketing
          </a>
        </div>
      </footer>
    </div>
  );
}
