"use client";

import { LOCALES, toAppLocale, type AppLocale } from "@ticket-pos/locale";
import { cn } from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useTransition } from "react";

import { localeChoiceCookie } from "@/lib/staff-locale";

/**
 * Each language named in its own words, and deliberately not in the catalog.
 *
 * "Español" is not a translation of "Spanish" — it is the label a Spanish reader
 * scans a page for, and they are scanning a page written in English. Putting
 * these in messages/*.json would make them per-locale by construction: the
 * English page would say "English / Spanish" and the only person who needed the
 * switcher would not find their language on it.
 */
const LANGUAGE_ENDONYMS: Record<AppLocale, string> = {
  en: "English",
  es: "Español",
};

/**
 * The language switcher: both languages, always, as buttons.
 *
 * Buttons and not links, which is the visible consequence of having no `[locale]`
 * segment. The Storefront's switcher is a pair of anchors because there a
 * translation has an address of its own, and publishing it is half the point. In
 * this app there is no other address to point at: the same URL renders in
 * whichever language the reader is owed, so the only thing a click can do is
 * change who the reader is understood to be and ask the server again.
 *
 * A click writes NEXT_LOCALE and then refreshes. Both halves matter: the cookie
 * is what makes the choice outlive this page, and the refresh is what makes the
 * page they are looking at change immediately rather than at the next
 * navigation — on the login page, "immediately" is the only feedback that the
 * click did anything at all.
 *
 * This is the login half of ADR 0041's pair, and it writes the cookie ONLY.
 * There is nobody signed in on this surface, so there is no person to store a
 * Staff Locale against. The shell switcher, when it lands, writes the stored
 * value as well — the two are deliberately not the same control and should not
 * be merged.
 *
 * A client component because writing a cookie and refreshing are both things
 * only the browser can do; it server-renders like anything else, so both buttons
 * are in the first byte of HTML.
 */
export function LanguageSwitcher() {
  const t = useTranslations("shell");
  const current = toAppLocale(useLocale());
  const router = useRouter();
  const [pending, startTransition] = useTransition();

  function choose(locale: AppLocale) {
    // Written even when the language does not change: clicking "English" while
    // reading English is still somebody stating what they want, and it is the
    // only way a reader who was routed here by Accept-Language can pin it.
    document.cookie = localeChoiceCookie(locale, {
      secure: window.location.protocol === "https:",
    });
    // The server re-resolves the locale from the cookie it has just been handed
    // and re-renders. Nothing is passed to it: the language is not in the URL,
    // so there is nothing to navigate to.
    startTransition(() => router.refresh());
  }

  return (
    <nav aria-label={t("languageSwitcherLabel")}>
      <ul className="flex items-center justify-center gap-4 text-sm">
        {LOCALES.map((locale) => {
          const active = locale === current;
          return (
            <li key={locale}>
              <button
                type="button"
                // The current language stays a button and says so, rather than
                // going missing or turning into text: a reader needs to see
                // which of the two they are in, and pressing it is how they pin
                // a guess that happened to be right.
                aria-current={active ? "true" : undefined}
                disabled={pending}
                onClick={() => choose(locale)}
                // The label is in that language, whatever the page is in, so a
                // screen reader pronounces "Español" as Spanish.
                lang={locale}
                className={cn(
                  "rounded-sm underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2",
                  active
                    ? "font-medium text-foreground"
                    : "text-muted-foreground underline hover:text-foreground",
                )}
              >
                {LANGUAGE_ENDONYMS[locale]}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
