import { StorefrontShell as BaseStorefrontShell } from "@ticket-pos/ui";
import { getLocale } from "next-intl/server";
import type { ComponentProps } from "react";

import { localizedPath, toAppLocale } from "@/lib/locale";

/**
 * The shared Storefront shell, wearing the locale of the page inside it.
 *
 * The shell in @ticket-pos/ui knows nothing about locales and should not: it is
 * markup shared with Staff, which has none. The one address it holds on its own
 * is the mark's link home, and that is exactly the link that must not silently
 * change language — a Customer reading Spanish who taps the logo and lands in
 * English has been thrown out of the Storefront they were in, because "/"
 * resolves by cookie and browser rather than by where they already were.
 *
 * So the rule lives here, once, rather than as a formula repeated at every page
 * that renders a shell. The locale comes from the request rather than from
 * props, which is what keeps the pages themselves unchanged apart from the
 * import.
 */
export async function StorefrontShell(
  props: Omit<ComponentProps<typeof BaseStorefrontShell>, "homeHref">,
) {
  const locale = toAppLocale(await getLocale());
  return <BaseStorefrontShell {...props} homeHref={localizedPath(locale, "/")} />;
}
