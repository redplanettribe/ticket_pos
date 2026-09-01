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
  requirePrivacyPolicy,
} from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import { toAppLocale } from "@/lib/locale";
import { PRIVACY_POLICY_PATH, PRIVACY_POLICY_PROTECTED_LOCALE } from "@/lib/privacy-policy";
import { storefrontBaseUrl } from "@/lib/site";

export const dynamic = "force-dynamic";

type PrivacyPolicyPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: PrivacyPolicyPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "privacy" });
  // The languages this notice is really published in, and the one language it
  // can never stop being published in (#559). x-default is a claim about where
  // a reader with no stated language lands, so on this path it names the
  // protected locale rather than English — English is a language an edition may
  // drop, and an x-default into a 404 is worse than none.
  //
  // A failed read leaves the annotation at every app locale: this page is about
  // to 500 anyway (requirePrivacyPolicy below), and a <head> nobody will see is
  // not worth a second failure mode.
  const published = await publishedLegalLocales("privacy-policy", {
    revalidate: LEGAL_PUBLICATION_REVALIDATE_SECONDS,
  });
  const { canonical, languages } = localeAlternates(
    PRIVACY_POLICY_PATH,
    toAppLocale(locale),
    storefrontBaseUrl(),
    { locales: published ?? undefined, xDefault: PRIVACY_POLICY_PROTECTED_LOCALE },
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
 * This page never renders with no policy on it — every other public page
 * degrades to less content, and this one cannot, because a Privacy Policy page
 * with no policy on it makes a promise the platform is not keeping (ADR 0036).
 *
 * WHICH failure it is now decides which honest answer it gets (#559). A 404
 * from the API means this language is genuinely not in the current edition's
 * published set, and Next's not-found page is a true statement about it. Every
 * other failure — a 5xx, an unreachable API, a body that is not the envelope —
 * throws out of requirePrivacyPolicy and reaches the error boundary as a 500,
 * because "the platform publishes no privacy policy" is a false statement and
 * the expensive one to have made to a crawler, a reader or a regulator.
 */
export default async function PrivacyPolicyPage({ params }: PrivacyPolicyPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const policy = await requirePrivacyPolicy(locale);
  if (!policy) {
    // Null means one thing only: a 404, so this language is not published.
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
