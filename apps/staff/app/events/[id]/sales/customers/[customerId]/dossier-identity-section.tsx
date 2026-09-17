"use client";

import { useTranslations } from "next-intl";

import type { CustomerDossier } from "@/lib/customer-dossier";

type DossierIdentitySectionProps = {
  customer: CustomerDossier["customer"];
};

/**
 * Who the person is to this Event. Only the email today: the name and Tax ID
 * are per Sale and live on each Sale's card, never merged into one here.
 */
export function DossierIdentitySection({ customer }: DossierIdentitySectionProps) {
  const t = useTranslations("customerDossier");

  return (
    <section aria-labelledby="dossier-identity" className="space-y-2">
      <h2 id="dossier-identity" className="text-base font-semibold">
        {t("identityHeading")}
      </h2>
      <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[max-content_1fr]">
        <dt className="text-muted-foreground">{t("emailLabel")}</dt>
        <dd className="break-all font-medium">{customer.email}</dd>
      </dl>
    </section>
  );
}
