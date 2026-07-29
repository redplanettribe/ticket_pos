import { createNavigation } from "next-intl/navigation";

import { routing } from "./routing";

/**
 * The Storefront's navigation, locale-aware.
 *
 * Every internal link and redirect goes through these rather than through
 * `next/link` and `next/navigation`, and the difference is the whole point: a
 * path written here is locale-free — "/tickets", "/signin?next=/tickets", an
 * Event's "/{orgSlug}/events/{eventSlug}" — and gains the locale the visitor is
 * already reading in on the way out. So a Customer browsing in Spanish stays in
 * Spanish through every link, and nothing in the codebase has to concatenate a
 * prefix by hand.
 *
 * `usePathname` reads the same way round: it returns the path without the
 * prefix, which is what lets the "sign in and come back here" links carry a
 * `next` that is not tied to one language.
 *
 * The exception is the unprefixed route handlers — /checkout/return,
 * /tickets/confirm and everything under /api — which have no locale to inherit
 * and must build one explicitly (see lib/redirect-locale.ts).
 */
export const { Link, redirect, usePathname, useRouter, getPathname } = createNavigation(routing);
