"use client";

import { NotFoundScreen } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { Link } from "@/i18n/navigation";

/**
 * The not-found screen (#799): an Event, Organization or page that calls
 * `notFound()`, and, through the catch-all beside it ([...rest]/page.tsx), an
 * address under a locale that no route matches. It renders inside the root
 * layout, whose next-intl provider took its locale off the address, so it speaks
 * the address's language and never the cookie's or the browser's (the
 * middleware's invariant), and Next still answers 404.
 *
 * A client component for the same reason the error screen is one: the
 * provider's locale is the layout's, settled before this renders, where a
 * server component here would read the request's locale with no params of its
 * own to read it from. The way back is the discover home of that same locale,
 * which the locale-aware Link prefixes.
 */
export default function NotFound() {
  const t = useTranslations("notFound");
  return (
    <NotFoundScreen
      labels={{ title: t("title"), description: t("description") }}
      backLink={<Link href="/">{t("back")}</Link>}
    />
  );
}
