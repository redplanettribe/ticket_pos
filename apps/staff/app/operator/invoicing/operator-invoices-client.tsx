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
  Button,
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
import { type AppLocale, formatCalendarDay, formatMoney } from "@/lib/format";
import {
  type InvoiceKind,
  type InvoiceKindFilter,
  type OperatorInvoiceListItem,
  fetchOperatorInvoices,
  fetchOperatorRecipientWarningCount,
} from "@/lib/operator-api";

import { CertificateExpiryBanner } from "./certificate-expiry-warning";
import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "./invoice-status";

// The invoices list (#454): every factura the platform issued, newest first —
// number, date, Recipient, total, status, country, and a Test badge for the
// SRI pruebas environment so a certification run is never mistaken for a real
// factura. From #473 it is also every document the platform OWES: a Sale
// Invoice or Credit Note appears the moment its sale commits, with its kind
// and its Sale Confirmation reference beside the manual Tax Invoices, and
// with no number or date until the Drainer signs it — those cells say so
// rather than showing a blank, since "not yet" and "unknown" are different.
// The kind filter (#477) narrows the list to one of the three; the API does
// the narrowing, so the page's total is the filtered total.
//
// The Recipient Warning (#482, ADR 0061): an authorized Sale Invoice the SRI
// warned about — the Recipient's Tax ID does not exist or is incorrect — is
// marked in the list and found by a second filter. Both exist only while
// Sale Invoicing is open: the count endpoint is the tell, answering 404 while
// the feature is closed, and the filter is drawn only once it has answered.

const KIND_FILTERS = ["all", "manual", "sale", "credit_note"] as const satisfies readonly InvoiceKindFilter[];

const SELECT_CLASS =
  "h-9 rounded-md border border-input bg-background px-3 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

function InvoiceRow({ item, locale }: { item: OperatorInvoiceListItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/invoicing/${item.id}`} className="font-mono font-medium hover:underline">
          {item.number ?? t("invoicingNotIssuedYet")}
        </Link>
        {item.environment === "test" ? (
          <Badge variant="outline" className="ml-2 align-middle">
            {t("invoicingTestBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="py-3 pr-4">{t(INVOICE_KIND_KEYS[item.kind])}</td>
      <td className="py-3 pr-4 font-mono text-xs">{item.sale_confirmation_ref ?? "—"}</td>
      <td className="py-3 pr-4 tabular-nums">
        {item.issued_on ? formatCalendarDay(item.issued_on, locale) : <span className="text-muted-foreground">—</span>}
      </td>
      <td className="py-3 pr-4">
        <p className="font-medium">{item.recipient.legal_name}</p>
        <p className="font-mono text-xs text-muted-foreground">{item.recipient.tax_id}</p>
      </td>
      <td className="py-3 pr-4 text-right tabular-nums">{formatMoney(item.total_cents, item.currency, locale)}</td>
      <td className="py-3 pr-4">
        <Badge variant={INVOICE_STATUS_VARIANTS[item.status]}>{t(INVOICE_STATUS_KEYS[item.status])}</Badge>
        {item.recipient_warning ? (
          // Beside the status, never in its place: the document is authorized
          // and the warning says something else about it.
          <Badge variant="outline" className="ml-2 border-amber-500 text-amber-700">
            {t("invoicingRecipientWarningBadge")}
          </Badge>
        ) : null}
        {item.superseded_by_invoice_id ? (
          // The superseded marker (#486, ADR 0061), beside the status for the
          // same reason: a reissued factura is still authorized, and no
          // longer the sale's current one — so the operator does not act on
          // it.
          <Badge variant="secondary" className="ml-2">
            {t("invoicingSupersededBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="py-3 pr-4 uppercase text-muted-foreground">{item.country}</td>
    </tr>
  );
}

export function OperatorInvoicesClient({
  initialRecipientWarningOnly = false,
}: {
  initialRecipientWarningOnly?: boolean;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorInvoiceListItem[]>([]);
  const [kind, setKind] = useState<InvoiceKindFilter>("all");
  const [recipientWarningOnly, setRecipientWarningOnly] = useState(initialRecipientWarningOnly);
  // null while unknown or while the feature is closed: the filter is not drawn.
  const [recipientWarningCount, setRecipientWarningCount] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // The count is read beside the page: it says whether Sale Invoicing is
      // open (404 while closed, read as "no filter") and how many the filter
      // would find. A closed feature must not take the list down with it.
      const [page, warningCount] = await Promise.all([
        fetchOperatorInvoices(1, kind, recipientWarningOnly),
        fetchOperatorRecipientWarningCount().catch(() => null),
      ]);
      setItems(page.data ?? []);
      setRecipientWarningCount(warningCount?.recipient_warning_count ?? null);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("invoicingListLoadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, recipientWarningOnly]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("invoicingBreadcrumbList") },
        ]}
      />
      <PageHeader
        title={t("invoicingListTitle")}
        description={t("invoicingListDescription")}
        actions={
          <>
            <Button asChild variant="outline">
              <Link href="/operator/invoicing/issuer">{t("invoicingViewIssuer")}</Link>
            </Button>
            <Button asChild>
              <Link href="/operator/invoicing/new">{t("invoicingNewInvoice")}</Link>
            </Button>
          </>
        }
      />

      {/*
        The Certificate Expiry Warning (#504, ADR 0063), where the documents
        are: the same banner the Operator Dashboard carries, read on its own
        so a failed Issuer read never takes the list down with it.
      */}
      <CertificateExpiryBanner />

      {forbidden ? (
        <Alert variant="destructive">
          <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
          <AlertDescription>{t("accessDenied")}</AlertDescription>
        </Alert>
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingListLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <p className="text-sm text-muted-foreground">{t("invoicingListLoading")}</p>
      ) : (
        <Card>
          <CardHeader className="flex flex-row items-start justify-between gap-4">
            <div>
              <CardTitle>{t("invoicingListTitle")}</CardTitle>
              <CardDescription>{t("invoicingListDescription")}</CardDescription>
            </div>
            <div className="flex flex-col items-end gap-2">
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">{t("invoicingKindFilterLabel")}</span>
                <select
                  className={SELECT_CLASS}
                  value={kind}
                  onChange={(event) => setKind(event.target.value as InvoiceKindFilter)}
                >
                  {KIND_FILTERS.map((option) => (
                    <option key={option} value={option}>
                      {option === "all" ? t("invoicingKindFilterAll") : t(INVOICE_KIND_KEYS[option as InvoiceKind])}
                    </option>
                  ))}
                </select>
              </label>
              {recipientWarningCount !== null ? (
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="h-4 w-4"
                    checked={recipientWarningOnly}
                    onChange={(event) => setRecipientWarningOnly(event.target.checked)}
                  />
                  <span>{t("invoicingRecipientWarningFilter", { count: recipientWarningCount })}</span>
                </label>
              ) : null}
            </div>
          </CardHeader>
          <CardContent>
            {items.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {recipientWarningOnly
                  ? t("invoicingListEmptyForRecipientWarning")
                  : kind === "all"
                    ? t("invoicingListEmpty")
                    : t("invoicingListEmptyForKind")}
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full border-collapse text-sm">
                  <thead>
                    <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <th className="py-2 pr-4 font-medium">{t("invoicingColNumber")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColKind")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColSale")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColDate")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColRecipient")}</th>
                      <th className="py-2 pr-4 text-right font-medium">{t("invoicingColTotal")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColStatus")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColCountry")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((item) => (
                      <InvoiceRow key={item.id} item={item} locale={locale} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
