import { Toaster } from "@ticket-pos/ui";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";
import { notFound } from "next/navigation";
import { NextIntlClientProvider, hasLocale } from "next-intl";
import { setRequestLocale } from "next-intl/server";

import { routing } from "@/i18n/routing";
import { storefrontBaseUrl } from "@/lib/site";

import "../globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-geist-sans",
});

// Default title for the global explorer at "/{locale}". Organization and Event
// pages set their own Organization-led titles via generateMetadata, so no title
// template is applied here — the product name must not intrude on those tabs.
export const metadata: Metadata = {
  // Resolves canonical and og:url to absolute URLs; undefined off-platform
  // (see lib/site.ts).
  metadataBase: storefrontBaseUrl(),
  title: "Multiticketing — Discover events",
};

// Neutral chrome on the Storefront: the visible space belongs to the
// Organization and its events, so mobile browser UI is not tinted brand blue.
export const viewport: Viewport = {
  themeColor: "#ffffff",
};

/**
 * The Storefront's root layout, and the only one: every page lives under a
 * locale, so this segment is the top of the tree for all of them and there is
 * no app/layout.tsx above it. What stays outside — the route handlers under
 * /api, /checkout/return and /tickets/confirm, plus the manifest and icon files
 * — are not pages and render no HTML, so they need no layout at all. A root
 * layout above this one could only add a second <html>.
 *
 * The locale segment is validated here rather than trusted. The middleware only
 * ever produces a supported one, but a URL is typed by hand and crawled: "/fr"
 * is a page that does not exist, and it says so rather than quietly rendering
 * English under a French address.
 */
export default async function LocaleLayout({
  children,
  params,
}: Readonly<{
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}>) {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) {
    notFound();
  }

  // Not for static rendering — every page here is force-dynamic and stays that
  // way. This is how next-intl's server APIs learn the locale at all when its
  // own middleware is not running: otherwise they look for an internal request
  // header that only that middleware sets, find nothing, and quietly answer
  // with the default. The symptom is silent and total — every server-rendered
  // link on a Spanish page points into English.
  //
  // It is repeated in every page rather than inherited from here, because a
  // layout and the page inside it render concurrently: this layout awaits its
  // params, and React renders the page while it waits. A page that does not
  // declare its own locale can therefore be rendered before this line runs.
  setRequestLocale(locale);

  return (
    // The URL's own token, not the Intl tag it formats under: the document
    // states the language it is written in, while "es-EC" is a claim about
    // number marks and month names that belongs to lib/format.ts.
    <html lang={locale}>
      <body className={`${inter.variable} surface-storefront font-sans`}>
        {/* Hands the locale down to client components, which is how a link
            rendered on the client knows which language to prefix. Its props are
            inherited from the server request config (i18n/request.ts). */}
        <NextIntlClientProvider>
          {children}
          <Toaster />
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
