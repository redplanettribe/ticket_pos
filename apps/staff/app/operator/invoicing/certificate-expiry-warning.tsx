"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Alert, AlertDescription, AlertTitle } from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";

import { certificateExpiryWarningVariant } from "@/lib/certificate-expiry";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { type OperatorCertificateExpiry, fetchOperatorEcuadorIssuer } from "@/lib/operator-api";

/**
 * The Certificate Expiry Warning as an Alert (#500, #504, ADR 0063 §5):
 * `warning` while more than seven days remain, `destructive` from seven days
 * out and once expired, nothing while the certificate is valid or absent —
 * "none" is the Issuer page's own copy, a different fact with a different
 * remedy. The state, the date and the day count are the API's; this never
 * counts days from the date, and the date is named in the platform's zone,
 * as the Issuer page names it.
 *
 * ONE COMPONENT FOR THREE SURFACES. The Issuer page draws it beside the
 * certificate's "valid until" row without a link — the remedy is the form
 * underneath. The Operator Dashboard and the invoicing list draw it with
 * `linkToIssuer`, so the remedy is one click from the warning wherever the
 * warning is seen.
 */
export function CertificateExpiryWarning({
  expiry,
  linkToIssuer = false,
  className,
}: {
  expiry: OperatorCertificateExpiry;
  linkToIssuer?: boolean;
  className?: string;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());

  const variant = certificateExpiryWarningVariant(expiry);
  if (variant === null || expiry.not_after === null || expiry.days_before === null) return null;
  const date = formatDateTime(expiry.not_after, PLATFORM_TIME_ZONE, locale) ?? expiry.not_after;
  const expired = expiry.state === "expired";

  return (
    <Alert variant={variant} className={className}>
      <AlertTitle>
        {expired ? t("invoicingCertificateExpiredTitle") : t("invoicingCertificateExpiringTitle")}
      </AlertTitle>
      <AlertDescription>
        {expired
          ? t("invoicingCertificateExpired", { date })
          : t("invoicingCertificateExpiring", { date, days: expiry.days_before })}
        {linkToIssuer ? (
          <>
            {" "}
            <Link href="/operator/invoicing/issuer" className="font-medium underline underline-offset-4">
              {t("invoicingCertificateExpiryOpenIssuer")}
            </Link>
          </>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}

/**
 * The Certificate Expiry Warning banner on the Operator Dashboard and the
 * invoicing list (#504): reads the Issuer on its own and draws the Alert
 * above, linked to the Issuer page.
 *
 * IT LOADS BESIDE THE PAGE, NOT WITH IT. The Issuer read is served whether or
 * not `SALE_INVOICING_ENABLED` is open (ADR 0063 §5) — the flag is closed in
 * production, and that is exactly the state the warning must be alive in —
 * but the read is not the page's own, and the banner must never take the page
 * down with it: a page whose Issuer read failed shows no banner, not an
 * error. "No Issuer" is `null` from the API and draws nothing for the same
 * reason "none" does.
 */
export function CertificateExpiryBanner() {
  const [expiry, setExpiry] = useState<OperatorCertificateExpiry | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchOperatorEcuadorIssuer()
      .then((issuer) => {
        if (!cancelled) setExpiry(issuer?.certificate_expiry ?? null);
      })
      .catch(() => {
        // Nothing to show: the page is not about the certificate, and a
        // failed read here is not the page's failure to report.
        if (!cancelled) setExpiry(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (expiry === null) return null;
  return <CertificateExpiryWarning expiry={expiry} linkToIssuer />;
}
