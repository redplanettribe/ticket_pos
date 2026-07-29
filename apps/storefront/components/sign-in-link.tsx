"use client";

import { Button } from "@ticket-pos/ui";

import { Link, usePathname } from "@/i18n/navigation";

/**
 * The header's way in, carrying where the visitor already is.
 *
 * Somebody who signs in from an Event page wants to come back to that Event, not
 * to their Customer Area — so the current path travels as `next`, through
 * whichever proof they use: the passcode form reads it, and Google Sign-In
 * carries it inside its state cookie all the way round the redirect.
 *
 * A client component only because the current path is not available to a server
 * component. It renders the same link it always did otherwise.
 *
 * The path it carries is locale-free — next-intl's usePathname strips the
 * prefix and its Link puts one back — so a `next` written here means the same
 * page in whichever language the visitor finishes signing in under.
 */
export function SignInLink() {
  const pathname = usePathname();
  // "/tickets" is already where sign-in goes by default, and pointing back at
  // "/signin" would be a loop.
  const carryNext = pathname && pathname !== "/tickets" && !pathname.startsWith("/signin");
  const href = carryNext ? `/signin?next=${encodeURIComponent(pathname)}` : "/signin";

  return (
    <Button asChild variant="ghost" size="sm">
      <Link href={href}>Sign in</Link>
    </Button>
  );
}
