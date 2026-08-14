/**
 * Which language a page is served in, and which regional tag it formats under.
 *
 * Two vocabularies meet here and must not be confused. A URL — and a stored
 * preference — carries a short token, "en" or "es", because it is read by
 * people, typed into chats and printed on posters. Intl carries a
 * language-and-region tag — "en-US", "es-EC" — because number marks, month
 * names and the position of a currency symbol are regional facts, not language
 * ones. This module owns the mapping between them, so no page has to guess
 * which of the two it is holding.
 *
 * It lives in a package rather than in either app because a Locale is one fact
 * about the platform: if the Storefront and the staff app each kept their own
 * copy, they could disagree about what "es" is, which language is the default,
 * or which regional tag Spanish formats under — and nothing would notice.
 *
 * A Locale decides words. It decides nothing about time or money: an Event's
 * times are drawn in the Event's own timezone and the Reversal Window's cutoff
 * in Ecuador's (ADR 0018), and a Ticket Type's currency is the Organization's.
 * Those arguments are passed separately by each app's formatting module and no
 * locale may reach them.
 *
 * Everything here is a pure function over strings, so the precedence chain that
 * decides where an unprefixed address lands can be tested without a request.
 */

/**
 * The languages the platform speaks, as a URL or a cookie spells them.
 *
 * On the Storefront both are always prefixed. There is no unprefixed default:
 * "/" is not English, it is an address with no language in it, and it redirects
 * to one. The alternative — English at "/" and Spanish at "/es" — makes one
 * language the unmarked case and leaves every English URL unable to say so,
 * which is exactly what breaks a shared link.
 */
export const LOCALES = ["en", "es"] as const;

/** A language as the URL spells it. */
export type AppLocale = (typeof LOCALES)[number];

/** Where a visitor lands when nothing about them says otherwise. */
export const DEFAULT_APP_LOCALE: AppLocale = "en";

/**
 * The cookie remembering a deliberate choice of language.
 *
 * It is written by a language switcher and by nothing else. No middleware and
 * not next-intl writes it as a side effect of a visit, because a cookie set
 * from a browser's Accept-Language would make a visitor's first accidental
 * landing stick for six months.
 *
 * The name is the one next-intl and Next's own i18n conventions use, so a
 * cookie set here is understood by anything else that reads one — including the
 * other app on a shared domain.
 */
export const LOCALE_COOKIE = "NEXT_LOCALE";

/**
 * The Intl language-and-region tag a value is rendered under.
 *
 * Named for Intl and not for the domain, because the domain's Locale is the
 * short token above — "en", "es" — and there is only one of those. This is the
 * other vocabulary: the tag `Intl.NumberFormat` and `Intl.DateTimeFormat` are
 * handed, which carries a region because a decimal mark and a currency symbol's
 * position are regional facts. This module owns the mapping between the two,
 * and nothing else should be holding both at once.
 */
export type IntlLocale = "en-US" | "es-EC";

/**
 * The Intl tag each URL token formats under. "es" means Ecuadorian Spanish
 * because Ecuador is the market this platform sells in; the token stays short
 * because a URL is read by people and "/es-EC/..." tells them nothing they
 * wanted to know.
 */
const INTL_LOCALES: Record<AppLocale, IntlLocale> = {
  en: "en-US",
  es: "es-EC",
};

export function isAppLocale(value: unknown): value is AppLocale {
  return typeof value === "string" && (LOCALES as readonly string[]).includes(value);
}

/**
 * The URL token as an AppLocale, falling back to the default for anything else.
 *
 * For the places that hold a locale typed only as a string — next-intl's
 * `getLocale()` and `useLocale()`, which are validated by the time they return
 * but not narrowed — rather than for guessing at unvalidated input.
 */
export function toAppLocale(value: unknown): AppLocale {
  return isAppLocale(value) ? value : DEFAULT_APP_LOCALE;
}

/** The Intl tag to format numbers, dates and currencies under. */
export function intlLocale(locale: AppLocale): IntlLocale {
  return INTL_LOCALES[locale];
}

/** The URL token an Intl tag belongs to — the mapping read the other way. */
export function appLocaleFromIntl(locale: IntlLocale): AppLocale {
  const match = LOCALES.find((candidate) => INTL_LOCALES[candidate] === locale);
  return match ?? DEFAULT_APP_LOCALE;
}

/**
 * Which language to send a visitor into when their address names none.
 *
 * The chain is: a deliberate choice they made before (the cookie), then what
 * their browser says it reads (Accept-Language), then English.
 *
 * THIS DECIDES A REDIRECT TARGET AND NOTHING ELSE. A prefixed URL's content
 * must never depend on it: /en/... serves English to everyone including
 * crawlers, /es/... serves Spanish to everyone, and the link one person sends
 * another renders the same page at both ends. The moment this function is
 * consulted while rendering a prefixed page, the same URL starts meaning
 * different things to different people and every shared address, cached page
 * and indexed result becomes a guess.
 */
export function resolveLocale(input: {
  cookie?: string | null;
  acceptLanguage?: string | null;
}): AppLocale {
  const chosen = input.cookie?.trim();
  // Only a token the platform serves counts. The cookie is ours, written by a
  // language switcher alone, so anything else in it is stale or hand-made and is
  // read past rather than honoured.
  if (isAppLocale(chosen)) {
    return chosen;
  }
  return matchAcceptLanguage(input.acceptLanguage) ?? DEFAULT_APP_LOCALE;
}

/**
 * The best supported language in an Accept-Language header, or null when it
 * names none.
 *
 * Region is dropped on the way in: "es-EC", "es-419" and "es" are all the
 * platform's Spanish, because there is only one Spanish to serve. Entries are
 * read in quality order rather than in written order, which is the only thing
 * that makes "en;q=0.8,es;q=0.9" mean Spanish.
 *
 * It never throws. The header is attacker-controlled and arrives malformed from
 * ordinary clients too, so anything unparseable is simply an entry that does not
 * vote — and a header that is entirely unparseable elects the default.
 */
export function matchAcceptLanguage(header: string | null | undefined): AppLocale | null {
  if (!header) return null;

  const ranked: Array<{ tag: string; quality: number; order: number }> = [];
  header.split(",").forEach((entry, order) => {
    const [rawTag, ...parameters] = entry.split(";");
    const tag = rawTag?.trim().toLowerCase() ?? "";
    if (!tag) return;

    let quality = 1;
    for (const parameter of parameters) {
      const [key, value] = parameter.split("=");
      if (key?.trim().toLowerCase() !== "q") continue;
      const parsed = Number.parseFloat(value ?? "");
      // A q nobody can read is not a q of 1: an entry that fails to state its
      // weight loses to every entry that stated one, rather than outranking
      // them by accident.
      quality = Number.isFinite(parsed) ? Math.min(Math.max(parsed, 0), 1) : 0;
    }
    if (quality <= 0) return;
    ranked.push({ tag, quality, order });
  });

  // Written order breaks ties, which is what the header itself means by listing
  // two languages at the same weight.
  ranked.sort((a, b) => b.quality - a.quality || a.order - b.order);

  for (const { tag } of ranked) {
    const language = tag.split("-")[0];
    if (isAppLocale(language)) return language;
  }
  return null;
}
