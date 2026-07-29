"use client";

import { Button } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { useSearchParams } from "next/navigation";

import { Link, usePathname } from "@/i18n/navigation";
import { signInHref } from "@/lib/destination";

/**
 * The header's way in, carrying where the visitor already is.
 *
 * Somebody who signs in from an Event page wants to come back to that Event, not
 * to their Customer Area — so the current address travels as `next`, through
 * whichever proof they use: the passcode form reads it, and Google Sign-In
 * carries it inside its state cookie all the way round the redirect.
 *
 * The ADDRESS and not the path: the query goes too. On the checkout success page
 * it is the Sale Confirmation reference, which exists nowhere else, and on the
 * explorer it is the visitor's search and filters. Which parts to carry, and
 * which destinations are not worth carrying at all, are decided by
 * lib/destination.ts and unit-tested there — a client component being the one
 * place in this app a test cannot reach.
 *
 * `useSearchParams` comes from next/navigation rather than from i18n/navigation,
 * which does not wrap it: a query has no locale to strip or restore, so there
 * would be nothing for a wrapper to do.
 */
export function SignInLink() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  // Named by the page it opens, from that page's own key, so the way in and
  // what it leads to cannot come to say two different things.
  const t = useTranslations("signin");
  const href = signInHref(pathname, searchParams.toString());

  return (
    <Button asChild variant="ghost" size="sm">
      <Link href={href}>{t("title")}</Link>
    </Button>
  );
}
