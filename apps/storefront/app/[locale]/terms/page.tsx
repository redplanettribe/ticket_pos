import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Markdown, PageHeader } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import { getTerms } from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import { toAppLocale } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";
import { TERMS_PATH } from "@/lib/terms";

export const dynamic = "force-dynamic";

type TermsPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: TermsPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "terms" });
  const { canonical, languages } = localeAlternates(
    TERMS_PATH,
    toAppLocale(locale),
    storefrontBaseUrl(),
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
 * ONE DIFFERENCE: the body is Spanish on BOTH locales. The Terms are published
 * in Spanish only and the Spanish text legally prevails over any translation
 * (§37), so the English page shows the one operative document rather than a
 * 404 — the backend serves the same Spanish payload under every locale. The
 * page's own chrome (heading, effective-date label) still follows the reader's
 * language like everything else.
 *
 * An API that is down 404s this page rather than showing an empty one, for the
 * privacy page's reason: a terms page that appears to publish nothing is worse
 * than one that is honestly missing.
 */
export default async function TermsPage({ params }: TermsPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const terms = await getTerms(locale);
  if (!terms) {
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
