/**
 * What an indexable Storefront page tells a crawler about its own address and
 * its translations.
 *
 * Three claims, and they only work together. The canonical is SELF-REFERENTIAL:
 * /es/acme canonicalises to /es/acme, never to /en/acme. Pointing a translation
 * at its English twin says "these are the same page, index the English one",
 * which is how a Spanish page disappears from Spanish results — the opposite of
 * the reason it exists. The languages map is RECIPROCAL: every locale's page
 * lists every locale including itself, because a crawler only trusts an
 * hreflang pair it can confirm from both ends. And x-default names ENGLISH,
 * which is where the middleware sends an address naming no language when
 * nothing about the visitor says otherwise (lib/locale.ts DEFAULT_APP_LOCALE) —
 * x-default is a claim about that fallback, so the two must not drift apart.
 *
 * Both of the last two are DEFAULTS and not laws, because the two legal
 * documents break them (#559): their published language set is data, and their
 * x-default is the protected locale rather than English — English is a language
 * a publish may drop, and an x-default naming a dropped language is a fallback
 * into a 404. See AlternatesOptions.
 *
 * Nothing here reads a request. It is a pure function over a path, a locale and
 * an origin, so the shape a crawler will be handed can be asserted in a unit
 * test rather than inferred from rendered HTML.
 *
 * Only indexable pages get any of this. The Customer Area and the checkout
 * terminal pages are `robots: { index: false }`, and annotating a page that is
 * not in the index says nothing to nobody.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import {
  DEFAULT_APP_LOCALE,
  LOCALES,
  localePrefixOf,
  localizedPath,
  type AppLocale,
} from "./locale.ts";

/**
 * The hreflang keys this Storefront emits: the URL's own short tokens plus
 * x-default.
 *
 * "es" and not "es-EC", even though money and dates are formatted under
 * es-EC (lib/format.ts). hreflang answers "who is this page written for", and
 * the answer is every Spanish reader — there is one Spanish here to serve, so
 * pinning the tag to Ecuador would decline the rest of them for no gain.
 */
export type AlternateLanguages = Partial<Record<AppLocale, string>> &
  Record<"x-default", string>;

export type LocaleAlternates = {
  /** This page's own address, in the locale it is being served in. */
  canonical: string;
  /** Every locale's address, including this one's, plus x-default. */
  languages: AlternateLanguages;
};

/**
 * The two facts a path may know better than this module does (#559).
 *
 * Almost every Storefront page exists in every locale, and for those nothing
 * here is passed: the defaults are "all of them" and "x-default is English".
 * The two legal documents are the exception, because their language set is
 * data rather than a constant — the Legal Center can publish an edition in
 * fewer languages than the app has — and because English is the wrong
 * x-default for them.
 */
export type AlternatesOptions = {
  /**
   * The locales this particular path is actually published in.
   *
   * Defaults to every app locale. Naming a subset drops the others from the
   * languages map entirely, which is the honest annotation: an hreflang
   * pointing at an address that 404s teaches a crawler to distrust the whole
   * set, and it cannot be confirmed from the other end because there is no
   * other end.
   */
  locales?: readonly AppLocale[];
  /**
   * The locale x-default names.
   *
   * Defaults to DEFAULT_APP_LOCALE, which is where the middleware sends an
   * address naming no language. The legal paths override it with their
   * protected locale — the one language the Legal Center refuses to let a
   * publish drop (the Policy's MandatoryLocale, the Terms' PrevailingLocale,
   * both Spanish). x-default is a claim about where a reader lands when
   * nothing else applies, and it must not name a language the document may
   * stop being published in.
   */
  xDefault?: AppLocale;
};

/**
 * The canonical and hreflang set for a locale-independent path.
 *
 * `path` is the address with no language in it — "/", "/acme",
 * "/acme/events/gala" — and carries no query string: a filtered or
 * parameterised view of a page is that page, and every one of them must name
 * the same canonical or the index fills with near-duplicates.
 *
 * `base` is the Storefront's origin (lib/site.ts), which is absent off-platform.
 * When it is, root-relative paths are returned and Next resolves them against
 * metadataBase exactly as it already does for the addresses these replace — the
 * annotation degrades to relative rather than to wrong.
 */
export function localeAlternates(
  path: string,
  locale: AppLocale,
  base?: URL,
  options: AlternatesOptions = {},
): LocaleAlternates {
  const bare = withoutLocalePrefix(path);
  const url = (target: AppLocale) => absolute(localizedPath(target, bare), base);

  const published = options.locales ?? LOCALES;
  const requested = options.xDefault ?? DEFAULT_APP_LOCALE;
  // x-default must name an address that answers. Asking for one this path is
  // not published in is a caller bug, and the recoverable answer is the first
  // language it IS published in rather than an hreflang into a 404.
  const fallback = published.includes(requested) ? requested : (published[0] ?? requested);

  const languages = Object.fromEntries([
    ...published.map((candidate) => [candidate, url(candidate)]),
    // The fallback the middleware itself applies, stated where a crawler can
    // read it.
    ["x-default", url(fallback)],
  ]) as AlternateLanguages;

  return { canonical: url(locale), languages };
}

/**
 * The path with its language token removed, if it had one.
 *
 * Callers build these paths from route params, and a page rendering at
 * /es/acme holds both the locale and the slug — so "/acme" and "/es/acme" both
 * arrive here in practice. Prefixing blindly would emit "/en/es/acme", an
 * address that 404s and would be published as this page's English twin.
 */
function withoutLocalePrefix(path: string): string {
  const normalized = path.startsWith("/") ? path : `/${path}`;
  const prefix = localePrefixOf(normalized);
  if (!prefix) return normalized;
  const rest = normalized.slice(`/${prefix}`.length);
  return rest.startsWith("/") ? rest : `/${rest}`;
}

function absolute(path: string, base: URL | undefined): string {
  if (!base) return path;
  // A base carrying a path of its own keeps it: `new URL("/x", base)` alone
  // would drop it and publish an address one directory too high.
  const mount = base.pathname.replace(/\/+$/, "");
  return new URL(`${mount}${path}`, base).toString();
}
