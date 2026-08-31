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
  /**
   * Where the mark links. Defaults to the app root, which is what a shell that
   * knows nothing about locales should point at; the Storefront passes a path
   * carrying the language the visitor is reading in, so the logo cannot drop
   * them out of it.
   */
  homeHref?: string;
  /** Alt text for an Organization's logo when it has no name to interpolate. */
  organizationLogoAlt?: string;
  /**
   * The Privacy Policy link in the footer: where it points and what it says.
   *
   * Both optional, and BOTH must be supplied for the link to render — an
   * address with no words is not a link anyone can read, and words with no
   * address are not a link at all. Staff has no privacy page of its own and
   * passes neither, leaving its footer exactly as it was.
   *
   * A plain path, like homeHref: this package knows nothing about locales, so
   * the caller passes an address already carrying the language its reader is
   * in. A Customer sent from a Spanish page to an English policy would be sent
   * to a document they cannot read, which for this particular document is the
   * whole failure.
   */
  privacyHref?: string;
  privacyLabel?: string;
  /**
   * The Terms and Conditions link in the footer, beside the Privacy Policy's:
   * where it points and what it says. Both optional and BOTH required to
   * render, a plain localized path — everything the privacy pair's comment
   * says holds here. Staff passes neither and its footer is untouched.
   */
  termsHref?: string;
  termsLabel?: string;
  /**
   * One more footer link, after Privacy Policy: where it points and what it
   * says.
   *
   * Both optional and BOTH required to render, for the reason the privacy pair
   * is. This one is the "Create an event" invitation into the staff app, whose
   * address is runtime configuration the Storefront may not have — so absence
   * is the ordinary case, not an error, and an absent link leaves the footer
   * byte for byte as it was: no separator, no gap, no placeholder. Staff passes
   * neither and is untouched.
   *
   * Unlike homeHref and privacyHref this is an absolute cross-origin URL, which
   * is why the caller resolves it rather than composing a path.
   */
  footerLinkHref?: string;
  footerLinkLabel?: string;
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
  homeHref = "/",
  privacyHref,
  privacyLabel,
  termsHref,
  termsLabel,
  footerLinkHref,
  footerLinkLabel,
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
            <a href={homeHref} aria-label={homeLinkLabel} className="inline-flex">
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
          {privacyHref && privacyLabel ? (
            <>
              <span aria-hidden className="px-2">
                ·
              </span>
              <a href={privacyHref} className="hover:text-foreground">
                {privacyLabel}
              </a>
            </>
          ) : null}
          {termsHref && termsLabel ? (
            <>
              <span aria-hidden className="px-2">
                ·
              </span>
              <a href={termsHref} className="hover:text-foreground">
                {termsLabel}
              </a>
            </>
          ) : null}
          {footerLinkHref && footerLinkLabel ? (
            <>
              <span aria-hidden className="px-2">
                ·
              </span>
              <a href={footerLinkHref} className="hover:text-foreground">
                {footerLinkLabel}
              </a>
            </>
          ) : null}
          {languageSwitcher ? <div className="mt-3 flex justify-center">{languageSwitcher}</div> : null}
        </div>
      </footer>
    </div>
  );
}
