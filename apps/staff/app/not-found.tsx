import { NotFoundScreen } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";
import Link from "next/link";

/**
 * The not-found screen (#799): an address no route matches, and every page that
 * calls `notFound()`. It renders inside the root layout, so the words are the
 * Staff Locale's (i18n/request.ts) and Next still answers 404, and it stands
 * alone with the logo, in place of a page that would have drawn its own shell.
 *
 * Operator pages call `notFound()` to hide that they exist from anyone else, so
 * this says only that there is nothing here: never that the page is forbidden,
 * gone, or anything else that would admit it is real. The way back is the
 * events list, which every signed-in Member can open.
 */
export default async function NotFound() {
  const t = await getTranslations("notFound");
  return (
    <NotFoundScreen
      labels={{ title: t("title"), description: t("description") }}
      backLink={<Link href="/events">{t("back")}</Link>}
    />
  );
}
