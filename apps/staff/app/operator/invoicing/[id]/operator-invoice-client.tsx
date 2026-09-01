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
import { invoiceLevers } from "@/lib/invoice-actions";
import { type OperatorInvoiceDetail, fetchOperatorInvoice } from "@/lib/operator-api";
import { recipientWarningMessages } from "@/lib/recipient-warning";

import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "../invoice-status";
import { OperatorInvoiceActions } from "./operator-invoice-actions";
import { OperatorInvoiceReissue } from "./operator-invoice-reissue";
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
//
// From #483 (ADR 0061) a Sale Invoice may be part of a REISSUE CHAIN: a
// corrected factura names the one it supersedes, a superseded factura names
// its corrected one, and the reissue's Credit Note credits the superseded
// one. The chain card links every direction the document has; the reissue
// trail card says who reissued, when and why; and a superseded factura is
// badged so, beside its still-authorized status — superseded is a relation,
// not a state. Reissue itself is operator-invoice-reissue.tsx's card, shown
// on the current authorized Sale Invoice alone.
//
// From #576 (ADR 0068) a refused document says WHICH refusal it met: the SRI
// refusing the number it carries — error 45, "secuencial registrado" — reads
// nothing like a schema error or a bad Tax ID, though all three park the
// document in the same status. `refused_by_number` is the API's derived
// answer and this page's banner is where the operator meets it.

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

// The four directions a document may link in (#476, #483), one sentence each.
type ChainLabel =
  | "invoicingDetailSupersedes"
  | "invoicingDetailSupersededBy"
  | "invoicingDetailCreditsLink"
  | "invoicingDetailCreditedByLink";

const OPERATION_KEYS = {
  submit: "invoicingAttemptSubmit",
  query: "invoicingAttemptQuery",
} as const;

// Every outcome the attempts ledger can hold, one label each. `unknown`
// (#514) is the SRI answering that it has no record of the clave — a row an
// operator must be able to tell from `received`, which is the SRI saying it
// holds the document; without its own label it would fall through to "Error",
// which it is not.
const OUTCOME_KEYS = {
  received: "invoicingAttemptOutcomeReceived",
  authorized: "invoicingAttemptOutcomeAuthorized",
  not_authorized: "invoicingAttemptOutcomeNotAuthorized",
  rejected: "invoicingAttemptOutcomeRejected",
  unknown: "invoicingAttemptOutcomeUnknown",
  error: "invoicingAttemptOutcomeError",
} as const;

/**
 * The Recipient Warning (#482, ADR 0061): the SRI authorized this factura and
 * said the Recipient's Tax ID does not exist or is incorrect. The document
 * stands; the card quotes the authority's own words — which of the stored
 * messages those are is `recipientWarningMessages`' decision — and says what
 * the warning means. Stays until the document is superseded, whatever else
 * happens to it.
 */
function RecipientWarningCard({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const quoted = recipientWarningMessages(invoice.messages, invoice.attempts);
  return (
    <Alert className="border-amber-500 text-amber-900 [&>svg]:text-amber-700">
      <AlertTitle>{t("invoicingRecipientWarningTitle")}</AlertTitle>
      <AlertDescription>
        <p>{t("invoicingRecipientWarningBody")}</p>
        {quoted.length === 0 ? (
          <p className="mt-2 text-muted-foreground">{t("invoicingRecipientWarningNoQuote")}</p>
        ) : (
          <ul className="mt-2 space-y-1">
            {quoted.map((message, index) => (
              <li key={`${message.identifier}-${index}`}>
                <span className="font-mono">{message.identifier}</span> {message.message}
                {message.additional_info ? (
                  <span className="text-muted-foreground"> — {message.additional_info}</span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </AlertDescription>
    </Alert>
  );
}

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
  const levers = invoiceLevers(invoice.status, signed, {
    kind: invoice.kind,
    superseded_by_invoice_id: invoice.superseded_by_invoice_id,
    credited_by_invoice_id: invoice.credited_by_invoice_id,
  });
  const chainLinks: { key: string; id: string; label: ChainLabel }[] = [];
  if (invoice.supersedes_invoice_id) {
    chainLinks.push({ key: "supersedes", id: invoice.supersedes_invoice_id, label: "invoicingDetailSupersedes" });
  }
  if (invoice.superseded_by_invoice_id) {
    chainLinks.push({ key: "superseded_by", id: invoice.superseded_by_invoice_id, label: "invoicingDetailSupersededBy" });
  }
  if (invoice.credits_invoice_id) {
    chainLinks.push({ key: "credits", id: invoice.credits_invoice_id, label: "invoicingDetailCreditsLink" });
  }
  if (invoice.credited_by_invoice_id) {
    chainLinks.push({ key: "credited_by", id: invoice.credited_by_invoice_id, label: "invoicingDetailCreditedByLink" });
  }
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
            {invoice.recipient_warning ? (
              <Badge variant="outline" className="border-amber-500 text-amber-700">
                {t("invoicingRecipientWarningBadge")}
              </Badge>
            ) : null}
            {invoice.superseded_by_invoice_id ? (
              <Badge variant="secondary">{t("invoicingSupersededBadge")}</Badge>
            ) : null}
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
            {invoice.credit_note_reason === "reissue" ? (
              <p className="text-muted-foreground">{t("invoicingDetailReissueReason")}</p>
            ) : invoice.credit_note_reason ? (
              <p className="text-muted-foreground">
                {t("invoicingDetailCreditNoteReason", {
                  route: t(
                    REVERSAL_ROUTE_KEYS[invoice.credit_note_reason as keyof typeof REVERSAL_ROUTE_KEYS] ??
                      "invoicingReversalRoutePlatform",
                  ),
                })}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {chainLinks.length > 0 ? (
        // The documents of a Sale link each other (#476, #483): a Credit
        // Note names the Sale Invoice it credits and a credited Sale Invoice
        // names its Credit Note; a corrected Sale Invoice names the one it
        // supersedes and a superseded one names its corrected one. One card,
        // one line per direction the document has.
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingDetailChainTitle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2 text-sm">
              {chainLinks.map((link) => (
                <li key={link.key} className="flex flex-wrap items-baseline gap-x-2">
                  <span className="text-muted-foreground">{t(link.label)}</span>
                  <Link href={`/operator/invoicing/${link.id}`} className="font-medium underline underline-offset-4">
                    {t("invoicingDetailOpenDocument")}
                  </Link>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      {invoice.reissued_by && invoice.reissued_at ? (
        // The reissue trail (#483): who reissued, when, and the note — shown
        // on the corrected factura, the superseded one and the Credit Note
        // alike. The operator's email and the moment are data; the sentence
        // is copy; the note is the operator's own words.
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingReissueTrailTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <p>
              {t("invoicingReissueTrail", {
                by: invoice.reissued_by,
                when: formatDateTime(invoice.reissued_at, PLATFORM_TIME_ZONE, locale) ?? invoice.reissued_at,
              })}
            </p>
            {invoice.reissue_note ? (
              <p>
                <span className="font-medium text-foreground">{t("invoicingReissueTrailNote")}</span>{" "}
                <span className="whitespace-pre-wrap">{invoice.reissue_note}</span>
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {invoice.refused_by_number ? (
        // The refusal by number (#576, ADR 0068): the SRI would not take this
        // document because of its secuencial, not because of anything in it.
        // Said here because the status alone cannot say it — a schema error
        // and a bad Tax ID park a document in exactly the same place — and
        // because the generic refusal copy reads as a fixable data problem,
        // which sent an operator resending 001-001-000000025 for two days.
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingNumberRefusalTitle")}</AlertTitle>
          <AlertDescription>{t("invoicingNumberRefusalBody")}</AlertDescription>
        </Alert>
      ) : null}

      {invoice.recipient_warning ? <RecipientWarningCard invoice={invoice} /> : null}

      {invoice.backfilled_by && invoice.backfilled_at ? (
        // The backfill trail (#509, ADR 0064): who owed the document by a
        // Sale Invoice Backfill and when, so the audit trail tells it from
        // one born at checkout. Absent on every other document.
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingBackfillTrailTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {t("invoicingBackfillTrail", {
              by: invoice.backfilled_by,
              when: formatDateTime(invoice.backfilled_at, PLATFORM_TIME_ZONE, locale) ?? invoice.backfilled_at,
            })}
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

      {invoice.abandoned_by && invoice.abandoned_at ? (
        // The abandonment trail (#578, ADR 0068): who recorded that the SRI
        // never took this document, when, and why. It stands where the
        // annulment trail does and says the opposite thing about the SRI, so
        // a reader of the page is never left to infer which of the two
        // happened; the number and the bytes it names are still on the page
        // above, which is the point of saying they are kept.
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingAbandonmentTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <p>
              {t("invoicingAbandonmentTrail", {
                by: invoice.abandoned_by,
                when: formatDateTime(invoice.abandoned_at, PLATFORM_TIME_ZONE, locale) ?? invoice.abandoned_at,
              })}
            </p>
            {invoice.abandon_note ? (
              <p>
                <span className="font-medium text-foreground">{t("invoicingAbandonmentTrailNote")}</span>{" "}
                <span className="whitespace-pre-wrap">{invoice.abandon_note}</span>
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <OperatorInvoiceActions invoice={invoice} onUpdated={setInvoice} />
      {levers.reissue ? <OperatorInvoiceReissue invoice={invoice} /> : null}

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
