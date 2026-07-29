import type { ReactNode } from "react";

import { LogoMark } from "./logo";
import { OrgAvatar } from "./org-avatar";

type StorefrontShellProps = {
  children: ReactNode;
  organizationName?: string;
  organizationLogoUrl?: string | null;
  /**
   * Customer sign-in state, rendered at the trailing edge of the header.
   * Optional: the shell knows nothing about Customer identity, so a surface
   * with no Customer chrome to show simply omits it and the header is exactly
   * as it was.
   */
  customerNav?: ReactNode;
  /**
   * A language switcher rendered in the footer. The shell owns no locale state
   * of its own — the Storefront supplies the control, Staff supplies nothing
   * and the footer is exactly as it was.
   */
  languageSwitcher?: ReactNode;
  /**
   * Copy overrides. Defaults are the English strings, so a caller that passes
   * none renders today's markup byte for byte. "Multiticketing" alone is the
   * brand name and is never translated.
   */
  homeLinkLabel?: string;
  poweredByLabel?: string;
  /** Alt text for an Organization's logo when it has no name to interpolate. */
  organizationLogoAlt?: string;
};

export function StorefrontShell({
  children,
  organizationName,
  organizationLogoUrl,
  customerNav,
  languageSwitcher,
  homeLinkLabel = "Multiticketing",
  poweredByLabel = "Powered by Multiticketing",
  organizationLogoAlt,
}: StorefrontShellProps) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-6xl items-center gap-2 px-4 py-2">
          {organizationName ? (
            <>
              <OrgAvatar
                logoUrl={organizationLogoUrl}
                name={organizationName}
                alt={organizationLogoAlt}
                shape="inline"
              />
              <p className="text-sm font-medium text-muted-foreground">{organizationName}</p>
            </>
          ) : (
            // Global explorer: just the Multiticketing mark (no wordmark),
            // linked home. The organizer owns the visible name on org pages.
            <a href="/" aria-label={homeLinkLabel} className="inline-flex">
              <LogoMark aria-hidden className="size-8 text-primary" />
            </a>
          )}
          {customerNav ? <div className="ml-auto flex items-center">{customerNav}</div> : null}
        </div>
      </header>
      <div className="flex-1">{children}</div>
      <footer className="border-t py-6">
        <div className="mx-auto w-full max-w-6xl px-4 text-center text-sm text-muted-foreground">
          <a href="https://multiticketing.com" className="hover:text-foreground">
            {poweredByLabel}
          </a>
          {languageSwitcher ? <div className="mt-3 flex justify-center">{languageSwitcher}</div> : null}
        </div>
      </footer>
    </div>
  );
}
