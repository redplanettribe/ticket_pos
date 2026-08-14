"use client";

import { LOCALES, toAppLocale, type AppLocale } from "@ticket-pos/locale";
import { Button, cn } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import { apiErrorMessage } from "@/lib/api-errors";
import { localeChoiceCookie } from "@/lib/staff-locale";

/**
 * Each language named in its own words, and deliberately not in the catalog.
 *
 * "Español" is not a translation of "Spanish" — it is the label a Spanish reader
 * scans a panel for, and they may be scanning a panel written in English. Putting
 * these in messages/*.json would make them per-locale by construction: the
 * English shell would say "English / Spanish" and the only person who needed the
 * switcher would not find their language on it.
 *
 * The same list the login switcher draws, for the same reason, and duplicated
 * rather than shared because the two controls are deliberately separate (below).
 */
const LANGUAGE_ENDONYMS: Record<AppLocale, string> = {
  en: "English",
  es: "Español",
};

/**
 * The language switcher in the app shell, and the SECOND of ADR 0041's pair.
 *
 * It writes two things where the login switcher writes one:
 *
 *   - the stored Staff Locale, through the API, because there is a person signed
 *     in now and the language belongs to them. That is what makes the choice
 *     survive signing out, appear on their next device, and word the email the
 *     platform sends them;
 *   - the NEXT_LOCALE cookie, because they will next see the sign-in page with no
 *     session at all, and a person who chose Spanish should not be greeted in
 *     English on the way back in.
 *
 * THE TWO SWITCHERS MUST NOT BE MERGED. The ADR says so outright, and the reason
 * is visible from here: `app/language-switcher.tsx` runs where nobody is signed
 * in, so there is no person to store a preference against and no endpoint that
 * would accept one. A single "simplified" control would either have to fail
 * silently on the login page or have the login page start writing a preference
 * for somebody it cannot name.
 *
 * WHERE IT SITS. Beside the organization switcher and the logout button — the
 * neighbourhood of controls about *you* rather than about the Organization — and
 * emphatically not on the Organization settings page. That surface is
 * Organization-scoped and administrative: an Event Staff member cannot open it,
 * and a Platform Operator who belongs to no Organization has no such page at all.
 * Both of them can reach this.
 *
 * WHY IT DOES NOT WAIT TO BE SURE. The cookie is written and the refresh is
 * started before the API answers, so the panel changes language in the same
 * moment as the click — the only feedback that says the click did anything. If
 * the write then fails the cookie is put back, the page re-renders in the old
 * language, and the failure is said out loud. Optimism here is cheap because the
 * cost of being wrong is a language, not a payment.
 */
export function ShellLanguageSwitcher() {
  const t = useTranslations("shell");
  const errorCopy = useMessages().errors;
  const current = toAppLocale(useLocale());
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  function writeCookie(locale: AppLocale) {
    document.cookie = localeChoiceCookie(locale, {
      secure: window.location.protocol === "https:",
    });
  }

  async function choose(locale: AppLocale) {
    setError(null);
    // Written even when the language does not change: clicking "English" while
    // reading English is still somebody stating what they want, and for a person
    // whose stored locale is null — never asked, never backfilled — it is how
    // they turn a detected guess into a stated fact.
    writeCookie(locale);
    startTransition(() => router.refresh());

    try {
      const response = await fetch("/api/staff/locale", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ locale }),
      });
      const envelope = (await response.json()) as {
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error) {
        writeCookie(current);
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("languageChangeFailed"));
        startTransition(() => router.refresh());
      }
    } catch {
      // The API was never reached, so nothing was stored and the cookie is a
      // claim the server cannot back up. Put it back.
      writeCookie(current);
      setError(t("languageChangeFailed"));
      startTransition(() => router.refresh());
    }
  }

  return (
    <div className="space-y-1">
      <nav aria-label={t("languageSwitcherLabel")}>
        <ul className="flex items-center gap-1">
          {LOCALES.map((locale) => {
            const active = locale === current;
            return (
              <li key={locale}>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  // The current language stays a button and says so, rather than
                  // going missing or turning into text: a reader needs to see
                  // which of the two they are in, and pressing it is how they pin
                  // a language that was only ever detected.
                  aria-current={active ? "true" : undefined}
                  disabled={pending}
                  onClick={() => void choose(locale)}
                  // The label is in that language, whatever the panel is in, so a
                  // screen reader pronounces "Español" as Spanish.
                  lang={locale}
                  className={cn(active ? "font-medium text-foreground" : "text-muted-foreground")}
                >
                  {LANGUAGE_ENDONYMS[locale]}
                </Button>
              </li>
            );
          })}
        </ul>
      </nav>
      {error ? (
        <p role="alert" className="px-3 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
