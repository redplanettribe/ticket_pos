"use client";

import { Inter } from "next/font/google";
import { usePathname } from "next/navigation";

import { ErrorScreenBody, type ErrorBoundaryProps } from "@/components/error-screen";
import { rootErrorScreenLocale } from "@/lib/error-screen";
import type { AppLocale } from "@/lib/locale";
import en from "@/messages/en.json";
import es from "@/messages/es.json";

import "./globals.css";

// The root layout's typeface, declared again: this document replaces that one.
const inter = Inter({
  subsets: ["latin"],
  variable: "--font-geist-sans",
});

/**
 * Only this screen's words, from the same catalogs as everywhere else. The
 * bundler keeps the one namespace read here and drops the rest of each catalog.
 */
const COPY: Record<AppLocale, typeof en.errorScreen> = {
  en: en.errorScreen,
  es: es.errorScreen,
};

/**
 * The root-level error screen (#774): the root layout (app/[locale]/layout.tsx)
 * itself crashed, so this renders the whole document in its place. It depends
 * on nothing the crash may have taken down: no next-intl provider (the words are
 * read straight from the catalogs), the language from the address as every
 * Storefront page takes it (lib/error-screen.ts), no Toaster, no Sentry meta or
 * loader (the screen reads the configuration before its document replaces the
 * layout's, and loads the SDK itself if it has to, components/error-screen.tsx).
 */
export default function GlobalError(props: ErrorBoundaryProps) {
  const locale = rootErrorScreenLocale(usePathname());

  return (
    <html lang={locale}>
      <body className={`${inter.variable} surface-storefront font-sans`}>
        <ErrorScreenBody {...props} copy={COPY[locale]} />
      </body>
    </html>
  );
}
