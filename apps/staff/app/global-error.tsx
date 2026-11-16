"use client";

import { DEFAULT_APP_LOCALE, type AppLocale } from "@ticket-pos/locale";
import { Inter } from "next/font/google";
import { useSyncExternalStore } from "react";

import { rootErrorScreenLocale } from "@/lib/error-screen";
import en from "@/messages/en.json";
import es from "@/messages/es.json";

import { ErrorScreenBody, type ErrorBoundaryProps } from "./error-screen";

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

function noSubscription() {
  return () => {};
}

/**
 * The root-level error screen (#772): the root layout itself crashed, so this
 * renders the whole document in its place. It depends on nothing the crash may
 * have taken down: no next-intl provider (the words are read straight from the
 * catalogs), no session (the language comes from the cookie and the browser,
 * lib/error-screen.ts), no Toaster, no Sentry meta (a browser SDK that started
 * before the crash still reports; one that did not stays dormant).
 */
export default function GlobalError(props: ErrorBoundaryProps) {
  const locale = useSyncExternalStore(
    noSubscription,
    () => rootErrorScreenLocale({ cookies: document.cookie, languages: navigator.languages }),
    () => DEFAULT_APP_LOCALE,
  );

  return (
    <html lang={locale}>
      <body className={`${inter.variable} surface-staff font-sans`}>
        <ErrorScreenBody {...props} layout="page" labels={COPY[locale]} />
      </body>
    </html>
  );
}
