import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Markdown, PageHeader } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import {
  LEGAL_PUBLICATION_REVALIDATE_SECONDS,
  publishedLegalLocales,
  requireTerms,
} from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import { toAppLocale } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";
import { TERMS_PATH, TERMS_PROTECTED_LOCALE } from "@/lib/terms";

export const dynamic = "force-dynamic";

type TermsPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: TermsPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "terms" });
  // The published set and the protected locale, on the Privacy Policy page's
  // rule and for its reasons (#559). Here the protected locale is the Spanish
  // text because it IS the contract (§37), so an x-default naming English would
  // point a reader with no stated language at the translation.
  const published = await publishedLegalLocales("terms", {
    revalidate: LEGAL_PUBLICATION_REVALIDATE_SECONDS,
  });
  const { canonical, languages } = localeAlternates(
    TERMS_PATH,
    toAppLocale(locale),
    storefrontBaseUrl(),
    { locales: published ?? undefined, xDefault: TERMS_PROTECTED_LOCALE },
  );
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    description: t("metaDescription", { brand: BRAND_NAME }),
    // Indexable, like the Privacy Policy and for its reason: a contract nobody
    // can find is most of the way to not having published one.
    alternates: { canonical, languages },
  };
}

/**
 * The full Términos y Condiciones Generales (#535, parent #533).
 *
 * Everything the Privacy Policy page's comment says holds here — the text
 * comes from the API and never from the message catalogs, because a Terms
 * Version records the SHA-256 of the exact text a person accepted, and the
 * page and the fingerprint must render the same bytes (ADR 0066,
 * backend/internal/consent/terms).
 *
 * Each locale renders its own text, like the Privacy Policy page: the Terms are
 * published in both, and the backend answers the locale in the URL strictly.
 * The two are ONE EDITION under one fingerprint, and the Spanish is the legally
 * prevailing text (§37) — the English body carries that notice in its own first
 * line, which is why this page needs no banner of its own and the footer below
 * can print the same hash on both.
 *
 * A language this edition does not publish 404s, and everything else that can
 * go wrong on the read throws a 500 instead (#559) — the privacy page's split,
 * for its reason: "this platform publishes no contract" is a false statement
 * and the expensive one to have made.
 */
export default async function TermsPage({ params }: TermsPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const terms = await requireTerms(locale);
  if (!terms) {
    // Null means one thing only: a 404, so this language is not published.
    notFound();
  }

  const t = await getTranslations("terms");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader
          title={t("title")}
          description={t("effectiveDate", { date: terms.effective_date })}
        />

        <Markdown className="text-foreground">{terms.body_markdown}</Markdown>

        {/*
          Which edition this is, and its fingerprint — the privacy page's
          footer, for its reason: what anyone accepts is a Terms Version, never
          "the terms", and the hash is the platform's own checkable claim about
          this text, the same value recorded on every acceptance of it.
        */}
        <p className="border-t pt-6 text-sm text-muted-foreground">
          {t("version", { version: terms.version })}
          <br />
          <span className="break-all font-mono text-xs">
            {t("contentHash", { hash: terms.content_hash })}
          </span>
        </p>
      </div>
    </StorefrontShell>
  );
}
