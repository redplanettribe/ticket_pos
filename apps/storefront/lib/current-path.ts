/**
 * The address the visitor is currently at, as a pure function over strings.
 *
 * Two client components need it and neither can be unit-tested: the language
 * switcher, which points at the other language's copy of THIS page, and the
 * header's sign-in link, which carries where the visitor already is so they come
 * back to it. Both have to be client components — only the browser's router
 * knows which page it is on — so the part that can be wrong lives here instead.
 *
 * It lives in its own module rather than in either caller's, because a shared
 * helper kept in one of them reads as that one's private business and the other
 * caller looks like it is reaching across for something that is not its own.
 */

/**
 * The address the visitor is at, language stripped off, query kept.
 *
 * Both halves matter. `pathname` arrives from next-intl's `usePathname`, which
 * has already removed the locale token, and is handed back to next-intl's `Link`
 * — so the result names a page rather than a page in a language, and whoever
 * consumes it decides which language that is.
 *
 * The query is carried because on this Storefront it IS the page. The explorer's
 * filters live in it, and so does the Sale Confirmation reference on the
 * checkout success page — the only copy of it the buyer's browser holds. A
 * caller that dropped the query would answer "sign in" or "read this in English"
 * with a different page than the one asked about, and on the success page with a
 * page that has nothing left to show at all.
 *
 * The fragment is not carried, because it cannot be: a fragment never leaves the
 * browser, so the server rendering this link has no way to know one exists and
 * writing it in on the client alone would make the markup differ between the two
 * renders.
 */
export function pathWithQuery(pathname: string, search?: string | null): string {
  // A path is normalized rather than trusted: `usePathname` returns "" before
  // the router has a route on some renders, and a href that is not root-relative
  // would be resolved against the current directory — silently producing
  // /es/acme/en/acme.
  const path = pathname.startsWith("/") ? pathname : `/${pathname}`;
  // `useSearchParams().toString()` returns a bare query with no "?", while
  // `location.search` includes one. Accepting both means the caller never has to
  // remember which it is holding.
  const query = (search ?? "").replace(/^\?/, "");
  // The lone slash is kept: "/" plus a query is "/?q=x", and next-intl's
  // prefixer is what collapses that to "/es?q=x" (shared/utils prefixPathname).
  return query ? `${path}?${query}` : path;
}
