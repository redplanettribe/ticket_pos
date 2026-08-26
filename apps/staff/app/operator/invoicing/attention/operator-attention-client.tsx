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
import { type AppLocale, PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { type OperatorNeedsAttentionItem, fetchOperatorNeedsAttention } from "@/lib/operator-api";

import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "../invoice-status";

// The documents that need an operator (#477, ADR 0060): every Sale Invoice
// or Credit Note parked needs_attention — refused by the SRI, unanswered
// for a day and still polled, or unsignable — longest waiting first, with
// its kind, its Sale Confirmation reference, since when, and what the SRI
// (or the platform, when nothing could be signed) last said. Each row opens
// the document, where Check status, Resend and Mark annulled live. The
// empty state is the ordinary one and says so; a stuck document is never
// silent, and neither is its absence.

function AttentionRow({ item, locale }: { item: OperatorNeedsAttentionItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b align-top last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/invoicing/${item.id}`} className="font-medium hover:underline">
          {t(INVOICE_KIND_KEYS[item.kind])}
        </Link>
        <p className="font-mono text-xs text-muted-foreground">{item.number ?? t("invoicingNotIssuedYet")}</p>
      </td>
      <td className="py-3 pr-4 font-mono text-xs">{item.sale_confirmation_ref ?? "—"}</td>
      <td className="py-3 pr-4">
        <Badge variant={INVOICE_STATUS_VARIANTS[item.status]}>{t(INVOICE_STATUS_KEYS[item.status])}</Badge>
      </td>
      <td className="py-3 pr-4 whitespace-nowrap">
        {formatDateTime(item.attention_since, PLATFORM_TIME_ZONE, locale) ?? "—"}
      </td>
      <td className="py-3 pr-4">
        {item.messages.length === 0 ? (
          <span className="text-muted-foreground">{t("invoicingAttentionNoMessages")}</span>
        ) : (
          <ul className="space-y-1">
            {item.messages.map((message, index) => (
              <li key={`${message.identifier}-${index}`}>
                <span className="font-mono text-xs">{message.identifier}</span> {message.message}
                {message.additional_info ? (
                  <span className="text-muted-foreground"> — {message.additional_info}</span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </td>
      <td className="py-3 text-right">
        <Button asChild size="sm" variant="outline">
          <Link href={`/operator/invoicing/${item.id}`}>{t("invoicingAttentionOpen")}</Link>
        </Button>
      </td>
    </tr>
  );
}

export function OperatorAttentionClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorNeedsAttentionItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await fetchOperatorNeedsAttention();
      setItems(page.data ?? []);
      setTotal(page.pagination.total);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("invoicingAttentionLoadFailed"),
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
          { label: t("invoicingBreadcrumbList"), href: "/operator/invoicing" },
          { label: t("invoicingAttentionBreadcrumb") },
        ]}
      />
      <PageHeader title={t("invoicingAttentionTitle")} description={t("invoicingAttentionDescription")} />

      {forbidden ? (
        <Alert variant="destructive">
          <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
          <AlertDescription>{t("accessDenied")}</AlertDescription>
        </Alert>
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingAttentionLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <p className="text-sm text-muted-foreground">{t("invoicingAttentionLoading")}</p>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingAttentionTitle")}</CardTitle>
            <CardDescription>{t("invoicingAttentionDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            {items.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("invoicingAttentionEmpty")}</p>
            ) : (
              <div className="overflow-x-auto">
                {/* The total is the API's; a page shows at most fifty. */}
                {total > items.length ? (
                  <p className="mb-2 text-xs text-muted-foreground">{`${items.length} / ${total}`}</p>
                ) : null}
                <table className="w-full border-collapse text-sm">
                  <thead>
                    <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <th className="py-2 pr-4 font-medium">{t("invoicingColKind")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColSale")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColStatus")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingAttentionColSince")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingAttentionColMessages")}</th>
                      <th className="py-2 font-medium" />
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((item) => (
                      <AttentionRow key={item.id} item={item} locale={locale} />
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
