"use client";

import Link from "next/link";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { rideOffer } from "@/lib/invoice-downloads";
import {
  type OperatorInvoiceDetail,
  operatorInvoiceAuthorizationXmlUrl,
  operatorInvoiceRidePath,
  operatorInvoiceRideUrl,
  operatorInvoiceSignedXmlUrl,
} from "@/lib/operator-api";

// The documents handed over (#456, #494): the signed XML in every status,
// the SRI's authorization XML once authorized, and the RIDE as a PDF once
// authorized (ADR 0062) — a Credit Note keeps the print view until #495.
// Plain links — the browser saves the files under the names the API gives
// them, and an unauthorized factura offers no RIDE at all rather than a
// link the API would refuse.

const linkClass = "inline-flex items-center rounded-md border px-3 py-1.5 text-sm font-medium hover:bg-muted";

export function InvoiceDownloads({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const ride = rideOffer(invoice);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingDownloadsTitle")}</CardTitle>
        <CardDescription>{t("invoicingDownloadsDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap gap-2">
          <a className={linkClass} href={operatorInvoiceSignedXmlUrl(invoice.id)} download>
            {t("invoicingDownloadSignedXml")}
          </a>
          {invoice.has_authorization_xml ? (
            <a className={linkClass} href={operatorInvoiceAuthorizationXmlUrl(invoice.id)} download>
              {t("invoicingDownloadAuthorizationXml")}
            </a>
          ) : null}
          {ride === "download" ? (
            <a className={linkClass} href={operatorInvoiceRideUrl(invoice.id)} download>
              {t("invoicingDownloadRide")}
            </a>
          ) : null}
          {ride === "print_view" ? (
            <Link className={linkClass} href={operatorInvoiceRidePath(invoice.id)} target="_blank" rel="noopener">
              {t("invoicingOpenRide")}
            </Link>
          ) : null}
        </div>
        {!invoice.has_authorization_xml ? (
          <p className="text-sm text-muted-foreground">{t("invoicingDownloadAuthorizationXmlUnavailable")}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}
