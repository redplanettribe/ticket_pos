"use client";

import { useLocale, useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";

import { Link, usePathname } from "@/i18n/navigation";
import { pathWithQuery } from "@/lib/current-path";
import { localeChoiceCookie } from "@/lib/language-switch";
import { LOCALES, toAppLocale, type AppLocale } from "@/lib/locale";

/**
 * Each language named in its own words, and deliberately not in the catalog.
 *
 * "Español" is not a translation of "Spanish" — it is the label a Spanish reader
 * scans a page for, and they are scanning a page written in English. Putting
 * these in messages/*.json would make them per-locale by construction: the
 * English page would say "English / Spanish" and the only person who needed the
 * switcher would not find their language on it. They are a fixed pair, the same
 * on every page in every language, so they live where that is unmistakable
 * rather than in a file whose whole purpose is being rewritten per locale.
 */
const LANGUAGE_ENDONYMS: Record<AppLocale, string> = {
  en: "English",
  es: "Español",
};

/**
 * The footer's language switcher: both languages, always, as real links.
 *
 * BOTH are anchors to this same page in that language, and both are in the DOM
 * on every render — including the one being read right now. That is not
 * decoration. An hreflang annotation (lib/alternates.ts) is a claim a crawler
 * may check; an ordinary internal link is how it finds the translation in the
 * first place, and how a reader who does not trust their browser's language
 * guess gets out of it. So: no <select>, no router.push, and no "show the other
 * one only" — a switcher that renders one link publishes half the site.
 *
 * A click also writes NEXT_LOCALE (lib/language-switch.ts), and this is the only
 * thing in the Storefront that writes it. The write happens in an onClick, not
 * on the server, because the alternative would be pointing the anchor at a
 * redirecting endpoint — and then the href a crawler follows is that endpoint
 * rather than the Spanish page, which is precisely the link that had to exist.
 * With the handler, the anchor's href IS the destination: it works with
 * JavaScript off, it survives being copied, and the cookie is a side effect that
 * can fail without costing anyone the navigation. All it decides is where an
 * address naming no language sends this visitor later.
 *
 * A client component only because the current path and query are not available
 * to a server component; it server-renders like any other, so the anchors are in
 * the first byte of HTML.
 */
export function LanguageSwitcher() {
  const t = useTranslations("shell");
  const current = toAppLocale(useLocale());
  // Locale-free, both of them: usePathname strips the prefix and Link puts the
  // target's back on, so this href means "this page" and not "this page in the
  // language I am already reading".
  const pathname = usePathname();
  const search = useSearchParams().toString();
  const href = pathWithQuery(pathname, search);

  function remember(locale: AppLocale) {
    // Written even when the language does not change: clicking "English" while
    // reading English is still somebody stating what they want, and it is the
    // only way a visitor who was routed here by Accept-Language can pin it.
    document.cookie = localeChoiceCookie(locale, {
      secure: window.location.protocol === "https:",
    });
  }

  return (
    <nav aria-label={t("languageSwitcherLabel")}>
      <ul className="flex items-center justify-center gap-4">
        {LOCALES.map((locale) => {
          const active = locale === current;
          return (
            <li key={locale}>
              <Link
                href={href}
                locale={locale}
                // The current language stays a link and says so, rather than
                // going missing or turning into text: a crawler confirms an
                // hreflang pair from both ends, and a reader needs to see which
                // of the two they are in.
                aria-current={active ? "true" : undefined}
                // The label is in that language, whatever the page is in, so a
                // screen reader pronounces "Español" as Spanish.
                lang={locale}
                onClick={() => remember(locale)}
                className={
                  active
                    ? "font-medium text-foreground"
                    : "underline underline-offset-4 hover:text-foreground"
                }
              >
                {LANGUAGE_ENDONYMS[locale]}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
