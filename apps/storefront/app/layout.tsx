import { Toaster } from "@ticket-pos/ui";
import type { Metadata, Viewport } from "next";
import { Inter } from "next/font/google";

import { storefrontBaseUrl } from "@/lib/site";

import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-geist-sans",
});

// Default title for the global explorer at "/". Organization and Event pages
// set their own Organization-led titles via generateMetadata, so no title
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

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className={`${inter.variable} surface-storefront font-sans`}>
        {children}
        <Toaster />
      </body>
    </html>
  );
}
