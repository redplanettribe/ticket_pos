"use client";

import Link from "next/link";
import { Fragment, useCallback, useEffect, useState } from "react";

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
  cn,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { type OperatorNeedsAttentionItem, fetchOperatorNeedsAttention } from "@/lib/operator-api";

import { OperatorInvoiceReinstate } from "../[id]/operator-invoice-reinstate";
import { OperatorInvoiceAcknowledge } from "../[id]/operator-invoice-unrecorded";
import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "../invoice-status";

// The documents that need an operator (#477, ADR 0060): every Sale Invoice
// or Credit Note parked needs_attention — refused by the SRI, unanswered
// for a day and still polled, or unsignable — longest waiting first, with
// its kind, its Sale Confirmation reference, since when, and what the SRI
// (or the platform, when nothing could be signed) last said. Each row opens
// the document, where Check status, Resend and Mark annulled live. The
// empty state is the ordinary one and says so; a stuck document is never
// silent, and neither is its absence.
//
// AND, SINCE #581, IT IS A UNION AND NOT ONE STATUS: an `abandoned` Sale
// Invoice with no live successor, on a Ticket Sale that still stands, is
// listed here too — a Sale whose buyer holds no valid factura and which
// nothing yet owes one, the state a Sale sits in between Abandon (#578) and
// Issue again (#580). Nothing about this surface changed for it: the same
// page, the same envelope, the same copy, and the row opens the same detail
// where Issue again is the press that clears it. Only "since when" reads
// two fields — attention_since is cleared on an abandoned row, and
// abandoned_at is the instant its wait began, which is the instant the
// queue orders it by.
//
// AND, SINCE #746, an annulled factura whose Annulment Check stands at
// mismatch: the SRI still reports it authorized. The row says so in place of
// the SRI's messages, and waits since `mismatch_since`. The annulment stands;
// the row clears itself when a later ask reads ANULADO.
//
// AND, SINCE #749, once such a factura's Annulment Window has closed
// (`reinstate_admitted`), the row offers Reinstate in place: the same confirm
// as the detail's panel, the number typed in full with an optional note. A
// reinstated factura leaves the queue, which is reloaded.
//
// AND, SINCE #750, an authorized factura carrying an unacknowledged
// Unrecorded Annulment: the Closing Sweep found the SRI reporting it
// annulled, and the platform has no record. The row says so and waits since
// the sweep found it. Where a live Credit Note refuses Mark annulled
// (`acknowledge_admitted`) it offers the acknowledgement in place; otherwise
// the row opens the factura, where Mark annulled records it. Either act
// takes it out of the queue, which is reloaded.

function AttentionRow({
  item,
  locale,
  onSettled,
}: {
  item: OperatorNeedsAttentionItem;
  locale: AppLocale;
  onSettled: () => void;
}) {
  const t = useTranslations("operator");
  const [reinstating, setReinstating] = useState(false);
  const [acknowledging, setAcknowledging] = useState(false);
  const canReinstate = item.reinstate_admitted && item.number !== null;
  const canAcknowledge = item.attention_reason === "unrecorded_annulment" && item.acknowledge_admitted;
  const expanded = (reinstating && canReinstate) || (acknowledging && canAcknowledge);
  return (
    <Fragment>
      <tr className={cn("align-top", !expanded && "border-b last:border-b-0")}>
        <td className="py-3 pr-4">
          <Link href={`/operator/invoicing/${item.id}`} className="font-medium hover:underline">
            {t(INVOICE_KIND_KEYS[item.kind])}
          </Link>
          <p className="font-mono text-xs text-muted-foreground">{item.number ?? t("invoicingNotIssuedYet")}</p>
        </td>
        <td className="py-3 pr-4 font-mono text-xs">{item.sale_confirmation_ref ?? "-"}</td>
        <td className="py-3 pr-4">
          <Badge variant={INVOICE_STATUS_VARIANTS[item.status]}>{t(INVOICE_STATUS_KEYS[item.status])}</Badge>
        </td>
        <td className="py-3 pr-4 whitespace-nowrap">
          {formatDateTime(
            item.attention_since ??
              item.abandoned_at ??
              item.mismatch_since ??
              item.annulled_at ??
              item.unrecorded_annulment?.found_at ??
              null,
            PLATFORM_TIME_ZONE,
            locale,
          ) ?? "-"}
        </td>
        <td className="py-3 pr-4">
          {item.attention_reason === "annulment_mismatch" ? (
            <span className="font-medium text-destructive">{t("invoicingAttentionAnnulmentMismatch")}</span>
          ) : item.attention_reason === "unrecorded_annulment" ? (
            <span className="font-medium text-destructive">{t("invoicingAttentionUnrecordedAnnulment")}</span>
          ) : item.attention_reason === "annulled" ? (
            <span className="font-medium">{t("invoicingAttentionAnnulled")}</span>
          ) : item.messages.length === 0 ? (
            <span className="text-muted-foreground">{t("invoicingAttentionNoMessages")}</span>
          ) : (
            <ul className="space-y-1">
              {item.messages.map((message, index) => (
                <li key={`${message.identifier}-${index}`}>
                  <span className="font-mono text-xs">{message.identifier}</span> {message.message}
                  {message.additional_info ? (
                    <span className="text-muted-foreground"> - {message.additional_info}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </td>
        <td className="space-x-2 whitespace-nowrap py-3 text-right">
          {canAcknowledge ? (
            <Button size="sm" variant="outline" onClick={() => setAcknowledging((open) => !open)}>
              {acknowledging ? t("invoicingUnrecordedAcknowledgeCancel") : t("invoicingUnrecordedAcknowledgeOpen")}
            </Button>
          ) : null}
          {canReinstate ? (
            <Button size="sm" variant="outline" onClick={() => setReinstating((open) => !open)}>
              {reinstating ? t("invoicingReinstateCancel") : t("invoicingReinstateOpen")}
            </Button>
          ) : null}
          <Button asChild size="sm" variant="outline">
            <Link href={`/operator/invoicing/${item.id}`}>{t("invoicingAttentionOpen")}</Link>
          </Button>
        </td>
      </tr>
      {reinstating && canReinstate && item.number !== null ? (
        <tr className="border-b last:border-b-0">
          <td colSpan={6} className="pb-4">
            <div className="ml-auto max-w-xl">
              <OperatorInvoiceReinstate invoiceId={item.id} number={item.number} onReinstated={onSettled} />
            </div>
          </td>
        </tr>
      ) : null}
      {acknowledging && canAcknowledge ? (
        <tr className="border-b last:border-b-0">
          <td colSpan={6} className="pb-4">
            <div className="ml-auto max-w-xl space-y-2 rounded-lg bg-muted/40 p-4 text-left">
              <p className="text-xs text-muted-foreground">{t("invoicingUnrecordedAcknowledgeHint")}</p>
              <OperatorInvoiceAcknowledge invoiceId={item.id} onAcknowledged={onSettled} />
            </div>
          </td>
        </tr>
      ) : null}
    </Fragment>
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
                      <AttentionRow key={item.id} item={item} locale={locale} onSettled={() => void load()} />
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
