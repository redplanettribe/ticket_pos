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
import { type OperatorInvoiceListItem, fetchOperatorInvoices } from "@/lib/operator-api";

import { INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "./invoice-status";

// The invoices list (#454): every factura the platform issued, newest first —
// number, date, Recipient, total, status, country, and a Test badge for the
// SRI pruebas environment so a certification run is never mistaken for a real
// factura.

function InvoiceRow({ item, locale }: { item: OperatorInvoiceListItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/invoicing/${item.id}`} className="font-mono font-medium hover:underline">
          {item.number}
        </Link>
        {item.environment === "test" ? (
          <Badge variant="outline" className="ml-2 align-middle">
            {t("invoicingTestBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="py-3 pr-4 tabular-nums">{formatCalendarDay(item.issued_on, locale)}</td>
      <td className="py-3 pr-4">
        <p className="font-medium">{item.recipient.legal_name}</p>
        <p className="font-mono text-xs text-muted-foreground">{item.recipient.tax_id}</p>
      </td>
      <td className="py-3 pr-4 text-right tabular-nums">{formatMoney(item.total_cents, item.currency, locale)}</td>
      <td className="py-3 pr-4">
        <Badge variant={INVOICE_STATUS_VARIANTS[item.status]}>{t(INVOICE_STATUS_KEYS[item.status])}</Badge>
      </td>
      <td className="py-3 pr-4 uppercase text-muted-foreground">{item.country}</td>
    </tr>
  );
}

export function OperatorInvoicesClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorInvoiceListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await fetchOperatorInvoices();
      setItems(page.data ?? []);
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
  }, []);

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
          <CardHeader>
            <CardTitle>{t("invoicingListTitle")}</CardTitle>
            <CardDescription>{t("invoicingListDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            {items.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("invoicingListEmpty")}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full border-collapse text-sm">
                  <thead>
                    <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <th className="py-2 pr-4 font-medium">{t("invoicingColNumber")}</th>
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
