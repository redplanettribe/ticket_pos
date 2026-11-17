import { NotFoundScreen } from "@ticket-pos/ui";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";
import { headers } from "next/headers";
import Link from "next/link";
import { getTranslations } from "next-intl/server";

import { localizedPath } from "@/lib/locale";
import { notFoundLocale } from "@/lib/not-found";

import "./globals.css";

// The root layout's typeface, declared again: this document stands in for it.
const inter = Inter({
  subsets: ["latin"],
  variable: "--font-geist-sans",
});

export const viewport: Viewport = {
  themeColor: "#ffffff",
};

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations({ locale: notFoundLocale(await headers()), namespace: "notFound" });
  return { title: t("title") };
}

/**
 * The not-found screen for an address no route matches (#799), e.g.
 * `/es/a/b/c/d`. Next renders this as the whole document, because the root
 * layout is the `[locale]` one and an unmatched address never reaches it; so it
 * draws its own <html> and <body>, as that layout would, and Next answers 404
 * with the screen already in the HTML.
 *
 * It speaks the language the address names (lib/not-found.ts), never the
 * cookie's or the browser's, and leads back to that language's global explorer.
 * The link is a plain one rather than the locale-aware Link, which needs the
 * provider this document is outside of; the path is written with the locale
 * the same way the middleware writes it.
 *
 * An Event, Organization or page that calls `notFound()` is the `[locale]`
 * not-found screen's instead, inside the root layout.
 */
export default async function GlobalNotFound() {
  const locale = notFoundLocale(await headers());
  const t = await getTranslations({ locale, namespace: "notFound" });
  return (
    <html lang={locale}>
      <body className={`${inter.variable} surface-storefront font-sans`}>
        <NotFoundScreen
          labels={{ title: t("title"), description: t("description") }}
          backLink={<Link href={localizedPath(locale, "/")}>{t("back")}</Link>}
        />
      </body>
    </html>
  );
}
