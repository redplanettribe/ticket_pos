import { getLocale } from "next-intl/server";

import type { IntlLocale } from "@/lib/format";
import { intlLocale, toAppLocale } from "@/lib/locale";

/**
 * The server-side twin of useFormatLocale(): the Intl tag this request formats
 * its prices and dates under. See ./format-locale.ts for why the walk lives in
 * one place rather than at each call site.
 *
 * It reads the request's locale rather than taking one, so a page passes
 * nothing and cannot pass the wrong thing. Pages that hold `params.locale`
 * already declared it with `setRequestLocale`, which is what this reads back.
 *
 * Kept in a separate module from the hook because this one pulls in
 * `next-intl/server`, which a client bundle must never import.
 */
export async function getFormatLocale(): Promise<IntlLocale> {
  return intlLocale(toAppLocale(await getLocale()));
}
