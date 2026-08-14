import { Toaster } from "@ticket-pos/ui";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";
import { NextIntlClientProvider } from "next-intl";
import { getLocale } from "next-intl/server";

import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-geist-sans",
});

export const metadata: Metadata = {
  title: "Multiticketing Staff",
};

export const viewport: Viewport = {
  themeColor: "#0e84c1",
};

/**
 * The staff app's only root layout, and the only place the reader's language
 * reaches the document.
 *
 * There is no `[locale]` segment above this and there never will be (ADR 0041):
 * the locale is a property of the person, resolved from the cookie today and
 * from the stored Staff Locale once there is a session to read one from
 * (i18n/request.ts). So `getLocale()` here is not reading a URL — it is asking
 * next-intl what that resolution decided for this request.
 *
 * The provider is mounted here, once, for the whole application. It was mounted
 * app-wide from the first migrated surface rather than per surface, because a
 * list of which pages get a provider goes stale on the first ticket that forgets
 * it, with a crash inside `useTranslations` as the symptom. Every surface is
 * translated now (#293), so there is no list left to keep either way.
 */
export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const locale = await getLocale();

  return (
    // The short token the person chose, not the Intl tag it formats under: the
    // document states the language it is written in, while "es-EC" is a claim
    // about number marks and month names.
    <html lang={locale} suppressHydrationWarning>
      <body className={`${inter.variable} surface-staff font-sans`}>
        {/* Hands the locale and the messages to client components — the login
            form is one — so `useTranslations` finds something to say. Both are
            inherited from the server request config rather than passed, so a
            namespace added to the catalog by a later migration is available on
            the client without anyone remembering to list it here. */}
        <NextIntlClientProvider>
          {children}
          <Toaster />
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
