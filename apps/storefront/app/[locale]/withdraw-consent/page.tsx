import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { PageHeader } from "@ticket-pos/ui";

import { ConsentWithdrawalForm } from "@/components/consent-withdrawal-form";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { BRAND_NAME } from "@/lib/brand";

// Rendered per request. Nothing on it is cacheable: every step is a form this
// visitor is in the middle of, and there is no session here to vary on.
export const dynamic = "force-dynamic";

export async function generateMetadata({
  params,
}: WithdrawConsentPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "withdrawConsent" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    description: t("metaDescription", { brand: BRAND_NAME }),
    // Indexable, unlike the unsubscribe page beside it, and deliberately: that
    // address carries a signed token naming one person, this one carries nothing
    // at all. A page where anybody may exercise a right should be findable by
    // the person looking for it — which is the whole complaint this feature
    // answers.
  };
}

type WithdrawConsentPageProps = {
  params: Promise<{ locale: string }>;
};

/**
 * Withdrawing a consent without signing in (#270, parent #265, ADR 0039).
 *
 * WHY THIS PAGE EXISTS. Signing in withholds the session until the consent step,
 * and that step demands acceptance of the Policy Version in effect. Publishing a
 * new edition re-gates everybody, so a person who wants to take a consent back
 * was met with "to withdraw your consent, first accept this". This page is the
 * way round it: a passcode proves the address, and the withdrawal is made
 * against that proof alone.
 *
 * NOBODY IS SIGNED IN BY IT. No session is minted at any step, and no cookie is
 * set by either route it calls, so exercising a right on a borrowed machine
 * leaves nothing behind on it.
 *
 * THIS PAGE ACTS ON NOTHING. It renders a form and stops; every act is a press
 * inside the island below. Rendering a page about somebody's rights must never
 * itself be an act with consequences.
 */
export default async function WithdrawConsentPage({ params }: WithdrawConsentPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const t = await getTranslations("withdrawConsent");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />
        <ConsentWithdrawalForm />
      </div>
    </StorefrontShell>
  );
}
