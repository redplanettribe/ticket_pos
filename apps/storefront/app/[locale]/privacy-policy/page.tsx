import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Markdown, PageHeader } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { localeAlternates } from "@/lib/alternates";
import { getPrivacyPolicy } from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import { toAppLocale } from "@/lib/locale";
import { PRIVACY_POLICY_PATH } from "@/lib/privacy-policy";
import { storefrontBaseUrl } from "@/lib/site";

export const dynamic = "force-dynamic";

type PrivacyPolicyPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: PrivacyPolicyPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "privacy" });
  const { canonical, languages } = localeAlternates(
    PRIVACY_POLICY_PATH,
    toAppLocale(locale),
    storefrontBaseUrl(),
  );
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    description: t("metaDescription", { brand: BRAND_NAME }),
    // Indexable, and it matters that it is: a privacy notice nobody can find is
    // most of the way to not having published one. Both languages are declared
    // to each other for the same reason every other indexable page does it.
    alternates: { canonical, languages },
  };
}

/**
 * The full Privacy Policy, in the reader's language (#250, parent #249).
 *
 * THE TEXT COMES FROM THE API AND NOT FROM THE MESSAGE CATALOGS, which is the
 * one thing worth knowing about this page. A Policy Version records the SHA-256
 * of the exact text a person was shown, and the only way for that to be
 * literally true is for the page and the fingerprint to render the same bytes:
 * the policy is embedded in the Go binary, hashed there, and served from there
 * (lib/api.ts getPrivacyPolicy, backend/internal/consent/policy). Copy in
 * `messages/*.json` is the page's own chrome — its heading, the label on the
 * effective date, the word "version" — none of which is part of the notice and
 * all of which is free to be worded per language like everything else.
 *
 * The body is markdown, rendered through the same component an Event's
 * description uses, so a heading or a table in the legal text lands as one
 * rather than as literal asterisks.
 *
 * An API that is down 404s this page rather than showing an empty one. Every
 * other public page degrades to less content; this one cannot, because a
 * Privacy Policy page with no policy on it makes a promise the platform is not
 * keeping — better a page that is honestly missing than a page that appears to
 * publish nothing at all.
 */
export default async function PrivacyPolicyPage({ params }: PrivacyPolicyPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const policy = await getPrivacyPolicy(locale);
  if (!policy) {
    notFound();
  }

  const t = await getTranslations("privacy");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader
          title={t("title")}
          description={t("effectiveDate", { date: policy.effective_date })}
        />

        <Markdown className="text-foreground">{policy.body_markdown}</Markdown>

        {/*
          Which edition this is, and its fingerprint. Not decoration: what a
          Customer accepts is a Policy Version and never "the policy", so the
          page they read it on has to say which one it is. The hash is shown
          because it is the platform's own claim about this text — anyone who
          kept a copy can check it — and it is the same value recorded on every
          acceptance of this edition.
        */}
        <p className="border-t pt-6 text-sm text-muted-foreground">
          {t("version", { version: policy.version })}
          <br />
          <span className="break-all font-mono text-xs">
            {t("contentHash", { hash: policy.content_hash })}
          </span>
        </p>
      </div>
    </StorefrontShell>
  );
}
