"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Breadcrumb,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatCalendarDay, formatDateTime, formatMoney } from "@/lib/format";
import { type OperatorInvoiceDetail, fetchOperatorInvoice } from "@/lib/operator-api";

import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "../invoice-status";
import { OperatorInvoiceActions } from "./operator-invoice-actions";
import { InvoiceDownloads } from "./invoice-downloads";

// One Tax Invoice in full (#454): Recipient, the Issuer as snapshotted, the
// lines and totals, the SRI's messages verbatim, the authorization number and
// date once authorized, and the attempts ledger. Check status and Resend are
// #455's card (operator-invoice-actions.tsx); downloads are #456.
//
// From #473 the document may be OWED and not yet signed — a Sale Invoice
// the checkout just wrote, waiting for the Drainer. Then there is no number,
// no clave, no Issuer snapshot, nothing to check, resend or download, and
// the page says so in each place rather than rendering an empty card: the
// kind badge and the Sale Confirmation reference are what identify it.

const IVA_RATE_KEYS = {
  "15": "invoicingIvaRate15",
  "0": "invoicingIvaRate0",
  exento: "invoicingIvaRateExento",
  no_objeto: "invoicingIvaRateNoObjeto",
} as const;

// The reversal route a Credit Note names as its reason (#476): the five
// words the Sales Export's reversed_by column uses, one label each. The
// one reason that is not a route — "reissue" (#481, ADR 0061), a Sale
// Invoice Reissue with no reversal behind it — is worded on its own below.
const REVERSAL_ROUTE_KEYS = {
  customer: "invoicingReversalRouteCustomer",
  platform: "invoicingReversalRoutePlatform",
  import_undo: "invoicingReversalRouteImportUndo",
  staff_reversal: "invoicingReversalRouteStaffReversal",
  correction: "invoicingReversalRouteCorrection",
} as const;

const OPERATION_KEYS = {
  submit: "invoicingAttemptSubmit",
  query: "invoicingAttemptQuery",
} as const;

const OUTCOME_KEYS = {
  received: "invoicingAttemptOutcomeReceived",
  authorized: "invoicingAttemptOutcomeAuthorized",
  not_authorized: "invoicingAttemptOutcomeNotAuthorized",
  rejected: "invoicingAttemptOutcomeRejected",
  error: "invoicingAttemptOutcomeError",
} as const;

export function OperatorInvoiceClient({ invoiceId }: { invoiceId: string }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [invoice, setInvoice] = useState<OperatorInvoiceDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setInvoice(await fetchOperatorInvoice(invoiceId));
    } catch (loadError) {
      setError(
        (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
          t("invoicingDetailLoadFailed"),
      );
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invoiceId]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("invoicingDetailLoading")}</p>;
  }
  if (error || !invoice) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("invoicingDetailLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("invoicingDetailLoadFailed")}</AlertDescription>
      </Alert>
    );
  }

  const money = (cents: number) => formatMoney(cents, invoice.currency, locale as AppLocale);
  const ivaLabel = (rate: string) =>
    t(IVA_RATE_KEYS[rate as keyof typeof IVA_RATE_KEYS] ?? "invoicingIvaRate0");
  const kindLabel = t(INVOICE_KIND_KEYS[invoice.kind]);
  const signed = invoice.ecuador !== null;
  const title = invoice.number
    ? t("invoicingDetailTitle", { number: invoice.number })
    : t("invoicingDetailTitleUnissued", { kind: kindLabel });

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("invoicingBreadcrumbList"), href: "/operator/invoicing" },
          { label: invoice.number ?? kindLabel },
        ]}
      />
      <PageHeader
        title={title}
        description={invoice.issued_on ? formatCalendarDay(invoice.issued_on, locale) : t("invoicingNotIssuedYet")}
        actions={
          <div className="flex items-center gap-2">
            <Badge variant="outline">{kindLabel}</Badge>
            {invoice.environment === "test" ? <Badge variant="outline">{t("invoicingTestBadge")}</Badge> : null}
            <Badge variant={INVOICE_STATUS_VARIANTS[invoice.status]}>{t(INVOICE_STATUS_KEYS[invoice.status])}</Badge>
          </div>
        }
      />

      {invoice.sale_confirmation_ref ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingDetailSale")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            <p className="font-mono font-medium">{invoice.sale_confirmation_ref}</p>
            {invoice.iva_rate ? (
              <p className="text-muted-foreground">{t("invoicingDetailPricedAt", { rate: ivaLabel(invoice.iva_rate) })}</p>
            ) : null}
            {invoice.reversal_reason === "reissue" ? (
              <p className="text-muted-foreground">{t("invoicingDetailReissueReason")}</p>
            ) : invoice.reversal_reason ? (
              <p className="text-muted-foreground">
                {t("invoicingDetailReversalReason", {
                  route: t(
                    REVERSAL_ROUTE_KEYS[invoice.reversal_reason as keyof typeof REVERSAL_ROUTE_KEYS] ??
                      "invoicingReversalRoutePlatform",
                  ),
                })}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {invoice.credits_invoice_id || invoice.credited_by_invoice_id ? (
        // The two documents of a reversed sale link each other (#476): a
        // Credit Note names the Sale Invoice it credits, and a credited Sale
        // Invoice names its Credit Note. One card either way; which sentence
        // it opens with says which side the reader is on.
        <Card>
          <CardHeader>
            <CardTitle>
              {invoice.credits_invoice_id ? t("invoicingDetailCredits") : t("invoicingDetailCreditedBy")}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-sm">
            <Link
              href={`/operator/invoicing/${invoice.credits_invoice_id ?? invoice.credited_by_invoice_id}`}
              className="font-medium underline underline-offset-4"
            >
              {t("invoicingDetailOpenDocument")}
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {invoice.annulled_by && invoice.annulled_at ? (
        // The annulment trail (#477): who recorded the portal act and when.
        // The operator's email and the moment are data; the sentence is copy.
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingAnnulmentTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {t("invoicingAnnulmentTrail", {
              by: invoice.annulled_by,
              when: formatDateTime(invoice.annulled_at, PLATFORM_TIME_ZONE, locale) ?? invoice.annulled_at,
            })}
          </CardContent>
        </Card>
      ) : null}

      <OperatorInvoiceActions invoice={invoice} onUpdated={setInvoice} />

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingDetailRecipient")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          <p className="font-medium">{invoice.recipient.legal_name}</p>
          <p className="font-mono text-muted-foreground">{invoice.recipient.tax_id}</p>
          {invoice.recipient.address ? <p className="text-muted-foreground">{invoice.recipient.address}</p> : null}
          {invoice.recipient.email ? <p className="text-muted-foreground">{invoice.recipient.email}</p> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingDetailLines")}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">{t("invoicingLineDescription")}</th>
                  <th className="py-2 pr-4 text-right font-medium">{t("invoicingLineQuantity")}</th>
                  <th className="py-2 pr-4 text-right font-medium">{t("invoicingLineUnitPrice")}</th>
                  <th className="py-2 pr-4 font-medium">{t("invoicingLineIvaRate")}</th>
                  <th className="py-2 pr-4 text-right font-medium">{t("invoicingTotalIva")}</th>
                  <th className="py-2 pr-4 text-right font-medium">{t("invoicingTotalSubtotal")}</th>
                </tr>
              </thead>
              <tbody>
                {invoice.lines.map((line) => (
                  <tr key={line.position} className="border-b last:border-b-0">
                    <td className="py-2 pr-4">{line.description}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{line.quantity}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{money(line.unit_price_cents)}</td>
                    <td className="py-2 pr-4">{ivaLabel(line.iva_rate)}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{money(line.iva_cents)}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{money(line.base_cents)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <dl className="mt-4 space-y-1 text-sm">
            <div className="flex justify-between">
              <dt className="text-muted-foreground">{t("invoicingTotalSubtotal")}</dt>
              <dd className="tabular-nums">{money(invoice.totals.subtotal_cents)}</dd>
            </div>
            {invoice.totals.discount_cents > 0 ? (
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("invoicingTotalDiscount")}</dt>
                <dd className="tabular-nums">{money(invoice.totals.discount_cents)}</dd>
              </div>
            ) : null}
            <div className="flex justify-between">
              <dt className="text-muted-foreground">{t("invoicingTotalIva")}</dt>
              <dd className="tabular-nums">{money(invoice.totals.iva_cents)}</dd>
            </div>
            <div className="flex justify-between font-medium">
              <dt>{t("invoicingTotalTotal")}</dt>
              <dd className="tabular-nums">{money(invoice.totals.total_cents)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingDetailAuthorization")}</CardTitle>
          {invoice.ecuador ? (
            <CardDescription className="break-all font-mono text-xs">
              {t("invoicingClave")}: {invoice.ecuador.access_key}
            </CardDescription>
          ) : null}
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          {!invoice.ecuador ? (
            <p className="text-muted-foreground">{t("invoicingAuthorizationNotIssued")}</p>
          ) : invoice.ecuador.authorization_number ? (
            <>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("invoicingAuthorizationNumber")}</dt>
                <dd className="break-all text-right font-mono text-xs">{invoice.ecuador.authorization_number}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("invoicingAuthorizationDate")}</dt>
                <dd className="text-right">
                  {formatDateTime(invoice.ecuador.authorization_date, PLATFORM_TIME_ZONE, locale)}
                </dd>
              </div>
            </>
          ) : (
            <p className="text-muted-foreground">{t("invoicingAuthorizationNone")}</p>
          )}
        </CardContent>
      </Card>

      {signed ? <InvoiceDownloads invoice={invoice} /> : null}

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingDetailMessages")}</CardTitle>
        </CardHeader>
        <CardContent>
          {invoice.messages.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("invoicingDetailNoMessages")}</p>
          ) : (
            <ul className="space-y-3 text-sm">
              {invoice.messages.map((message, index) => (
                <li key={`${message.identifier}-${index}`} className="rounded-md border p-3">
                  <p className="font-medium">
                    <span className="font-mono">{message.identifier}</span> {message.message}{" "}
                    <Badge variant="outline">{message.type}</Badge>
                  </p>
                  {message.additional_info ? (
                    <p className="mt-1 text-muted-foreground">{message.additional_info}</p>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingDetailAttempts")}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">{t("invoicingAttemptOperation")}</th>
                  <th className="py-2 pr-4 font-medium">{t("invoicingAttemptOutcome")}</th>
                  <th className="py-2 pr-4 font-medium">{t("invoicingAttemptWhen")}</th>
                  <th className="py-2 pr-4 text-right font-medium">{t("invoicingAttemptDuration")}</th>
                </tr>
              </thead>
              <tbody>
                {invoice.attempts.map((attempt) => (
                  <tr key={attempt.id} className="border-b last:border-b-0">
                    <td className="py-2 pr-4">{t(OPERATION_KEYS[attempt.operation] ?? "invoicingAttemptSubmit")}</td>
                    <td className="py-2 pr-4">{t(OUTCOME_KEYS[attempt.outcome] ?? "invoicingAttemptOutcomeError")}</td>
                    <td className="py-2 pr-4">{formatDateTime(attempt.started_at, PLATFORM_TIME_ZONE, locale)}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{attempt.duration_ms} ms</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      <Link href="/operator/invoicing" className="text-sm text-muted-foreground hover:underline">
        {t("invoicingBackToList")}
      </Link>
    </div>
  );
}
