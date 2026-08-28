"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Breadcrumb,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatDateTime, formatMoney, formatNumber } from "@/lib/format";
import {
  type BackfillRefusalCode,
  type OperatorBackfillResult,
  type OperatorPagination,
  type OperatorUninvoicedHouseSale,
  backfillOperatorUninvoicedHouseSales,
  fetchOperatorUninvoicedHouseSales,
} from "@/lib/operator-api";

// Ventas sin factura (#507, ADR 0064): every Uninvoiced House Sale platform-
// wide — a paid, active Online Sale of an Organization that is House NOW,
// with no Reversal Request in flight and no Sale Invoice row of any status
// — oldest sale first, fifty to a page. The list is the API's: the page
// never re-decides who is a candidate.
//
// The Sale Invoice Backfill (#508) is the page's one act: tick rows, press
// Facturar seleccionadas, confirm, and each selected sale is owed an
// ordinary Sale Invoice — dated the day it is issued, not the sale's day,
// which the page says before and during the act — and the Drainer is
// kicked. The API answers per sale: owed rows leave the list on the reload,
// refused rows are named with their reason in a banner. The selection is
// per page and cleared by any reload, so a stale id is never re-sent by
// accident; the API refuses it anyway with not_a_candidate.
//
// The feature is behind SALE_INVOICING_ENABLED: the list answers 404
// SALE_INVOICING_UNAVAILABLE while closed, and the page says so in place
// of the table rather than as a failure.

const SALE_INVOICING_UNAVAILABLE = "SALE_INVOICING_UNAVAILABLE";

const TAX_ID_TYPE_KEYS = {
  cedula: "invoicingUninvoicedTaxIdCedula",
  ruc: "invoicingUninvoicedTaxIdRuc",
  passport: "invoicingUninvoicedTaxIdPassport",
} as const;

const REFUSAL_KEYS: Record<BackfillRefusalCode, "invoicingUninvoicedRefusedNotACandidate" | "invoicingUninvoicedRefusedUnsupported"> = {
  not_a_candidate: "invoicingUninvoicedRefusedNotACandidate",
  unsupported_sale: "invoicingUninvoicedRefusedUnsupported",
};

function isUnavailable(error: unknown): boolean {
  return error instanceof ApiError && error.code === SALE_INVOICING_UNAVAILABLE;
}

/** The outcome banner's data: the API's result, joined back to the rows it was about. */
type BackfillOutcome = {
  owedCount: number;
  refused: { ticketSaleId: string; confirmationRef: string; code: BackfillRefusalCode }[];
};

function UninvoicedRow({
  item,
  locale,
  selected,
  disabled,
  onToggle,
}: {
  item: OperatorUninvoicedHouseSale;
  locale: AppLocale;
  selected: boolean;
  disabled: boolean;
  onToggle: (checked: boolean) => void;
}) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b align-top last:border-b-0">
      <td className="py-3 pr-2">
        <input
          type="checkbox"
          className="h-4 w-4"
          checked={selected}
          disabled={disabled}
          aria-label={t("invoicingUninvoicedSelectRow", { ref: item.confirmation_ref })}
          onChange={(event) => onToggle(event.target.checked)}
        />
      </td>
      <td className="py-3 pr-4 whitespace-nowrap tabular-nums">
        {formatDateTime(item.sold_at, PLATFORM_TIME_ZONE, locale) ?? item.sold_at}
      </td>
      <td className="py-3 pr-4">{item.organization_name}</td>
      <td className="py-3 pr-4">{item.event_name}</td>
      <td className="py-3 pr-4">{item.buyer_name}</td>
      <td className="py-3 pr-4">
        {item.buyer_tax_id_type && item.buyer_tax_id_number ? (
          <>
            <span className="text-muted-foreground">{t(TAX_ID_TYPE_KEYS[item.buyer_tax_id_type])}</span>{" "}
            <span className="font-mono text-xs">{item.buyer_tax_id_number}</span>
          </>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="py-3 pr-4 text-right tabular-nums">{formatMoney(item.total_cents, item.currency, locale)}</td>
      <td className="py-3 pr-4 font-mono text-xs">
        <Link href={`/operator/sales/${encodeURIComponent(item.confirmation_ref)}`} className="hover:underline">
          {item.confirmation_ref}
        </Link>
      </td>
    </tr>
  );
}

export function OperatorUninvoicedClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorUninvoicedHouseSale[]>([]);
  const [pagination, setPagination] = useState<OperatorPagination | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The selection, by ticket_sale_id, only ever among the rows on screen.
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [confirming, setConfirming] = useState(false);
  const [backfilling, setBackfilling] = useState(false);
  const [outcome, setOutcome] = useState<BackfillOutcome | null>(null);
  const [backfillError, setBackfillError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchOperatorUninvoicedHouseSales(page);
      setItems(result.data ?? []);
      setPagination(result.pagination);
      setSelected(new Set());
      setForbidden(false);
      setUnavailable(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else if (isUnavailable(loadError)) {
        // The flag is closed: not a failure, and not a page to offer.
        setUnavailable(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("invoicingUninvoicedLoadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page]);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedItems = useMemo(() => items.filter((item) => selected.has(item.ticket_sale_id)), [items, selected]);
  const allOnPageSelected = items.length > 0 && selectedItems.length === items.length;
  // One currency per selection is the House Organizations' reality today
  // (USD); should it ever not be, the dialog totals what it can and says so
  // by currency rather than adding apples to oranges.
  const selectedTotals = useMemo(() => {
    const byCurrency = new Map<string, number>();
    for (const item of selectedItems) {
      byCurrency.set(item.currency, (byCurrency.get(item.currency) ?? 0) + item.total_cents);
    }
    return [...byCurrency.entries()].map(([currency, cents]) => formatMoney(cents, currency, locale)).join(" + ");
  }, [selectedItems, locale]);

  const toggleAll = (checked: boolean) => {
    setSelected(checked ? new Set(items.map((item) => item.ticket_sale_id)) : new Set());
  };

  const toggleOne = (ticketSaleId: string, checked: boolean) => {
    setSelected((current) => {
      const next = new Set(current);
      if (checked) {
        next.add(ticketSaleId);
      } else {
        next.delete(ticketSaleId);
      }
      return next;
    });
  };

  const backfill = async () => {
    // The ids in row order, so the API's request-ordered answer reads the
    // same way the operator ticked them; the refs are kept beside them for
    // the banner, since the list is about to be reloaded without them.
    const chosen = selectedItems.map((item) => ({ id: item.ticket_sale_id, ref: item.confirmation_ref }));
    if (chosen.length === 0) {
      return;
    }
    setBackfilling(true);
    setBackfillError(null);
    setOutcome(null);
    try {
      const result: OperatorBackfillResult = await backfillOperatorUninvoicedHouseSales(chosen.map((c) => c.id));
      const refByID = new Map(chosen.map((c) => [c.id, c.ref]));
      setOutcome({
        owedCount: result.owed?.length ?? 0,
        refused: (result.refused ?? []).map((refusal) => ({
          ticketSaleId: refusal.ticket_sale_id,
          confirmationRef: refByID.get(refusal.ticket_sale_id) ?? refusal.ticket_sale_id,
          code: refusal.code,
        })),
      });
      setConfirming(false);
      await load();
    } catch (actError) {
      setConfirming(false);
      if (isUnavailable(actError)) {
        setBackfillError(t("invoicingUninvoicedBackfillUnavailable"));
      } else {
        setBackfillError(
          (actError instanceof ApiError ? apiErrorMessage(errorCopy, actError) : null) ??
            t("invoicingUninvoicedBackfillFailed"),
        );
      }
    } finally {
      setBackfilling(false);
    }
  };

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("invoicingBreadcrumbList"), href: "/operator/invoicing" },
          { label: t("invoicingUninvoicedBreadcrumb") },
        ]}
      />
      <PageHeader title={t("invoicingUninvoicedTitle")} description={t("invoicingUninvoicedDescription")} />

      {forbidden ? (
        <Alert variant="destructive">
          <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
          <AlertDescription>{t("accessDenied")}</AlertDescription>
        </Alert>
      ) : unavailable ? (
        <Alert>
          <AlertTitle>{t("invoicingUninvoicedUnavailableTitle")}</AlertTitle>
          <AlertDescription>{t("invoicingUninvoicedUnavailable")}</AlertDescription>
        </Alert>
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingUninvoicedLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : (
        <>
          <Alert>
            <AlertTitle>{t("invoicingUninvoicedDatedTodayTitle")}</AlertTitle>
            <AlertDescription>{t("invoicingUninvoicedDatedTodayNotice")}</AlertDescription>
          </Alert>

          {backfillError ? (
            <Alert variant="destructive">
              <AlertTitle>{t("invoicingUninvoicedBackfillFailedTitle")}</AlertTitle>
              <AlertDescription>{backfillError}</AlertDescription>
            </Alert>
          ) : null}

          {outcome ? (
            // The act's outcome, per sale: how many are now owed, and each
            // refused one by its reference with the API's reason in words.
            <Alert variant={outcome.refused.length === 0 ? "success" : "warning"}>
              <AlertTitle>{t("invoicingUninvoicedResultTitle")}</AlertTitle>
              <AlertDescription>
                <p>{t("invoicingUninvoicedResultOwed", { count: outcome.owedCount })}</p>
                {outcome.refused.length > 0 ? (
                  <>
                    <p className="mt-2 font-medium">
                      {t("invoicingUninvoicedResultRefused", { count: outcome.refused.length })}
                    </p>
                    <ul className="mt-1 list-disc space-y-1 pl-5">
                      {outcome.refused.map((refusal) => (
                        <li key={refusal.ticketSaleId}>
                          <Link
                            href={`/operator/sales/${encodeURIComponent(refusal.confirmationRef)}`}
                            className="font-mono text-xs hover:underline"
                          >
                            {refusal.confirmationRef}
                          </Link>{" "}
                          — {t(REFUSAL_KEYS[refusal.code])}
                        </li>
                      ))}
                    </ul>
                  </>
                ) : null}
              </AlertDescription>
            </Alert>
          ) : null}

          {loading ? (
            <p className="text-sm text-muted-foreground">{t("invoicingUninvoicedLoading")}</p>
          ) : (
            <Card>
              <CardHeader className="flex flex-row items-start justify-between gap-4">
                <div>
                  <CardTitle>{t("invoicingUninvoicedTitle")}</CardTitle>
                  <CardDescription>
                    {pagination
                      ? t("invoicingUninvoicedTotal", { total: pagination.total })
                      : t("invoicingUninvoicedDescription")}
                  </CardDescription>
                </div>
                {items.length > 0 ? (
                  <div className="flex items-center gap-3">
                    <span className="text-sm text-muted-foreground">
                      {t("invoicingUninvoicedSelectedCount", { count: selectedItems.length })}
                    </span>
                    <Button
                      type="button"
                      disabled={selectedItems.length === 0 || backfilling}
                      onClick={() => setConfirming(true)}
                    >
                      {t("invoicingUninvoicedInvoiceSelected")}
                    </Button>
                  </div>
                ) : null}
              </CardHeader>
              <CardContent>
                {items.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t("invoicingUninvoicedEmpty")}</p>
                ) : (
                  <div className="overflow-x-auto">
                    <table className="w-full border-collapse text-sm">
                      <thead>
                        <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                          <th className="py-2 pr-2 font-medium">
                            <input
                              type="checkbox"
                              className="h-4 w-4"
                              checked={allOnPageSelected}
                              disabled={backfilling}
                              aria-label={t("invoicingUninvoicedSelectAll")}
                              onChange={(event) => toggleAll(event.target.checked)}
                            />
                          </th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingUninvoicedColSoldAt")}</th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingUninvoicedColOrganization")}</th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingUninvoicedColEvent")}</th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingUninvoicedColBuyer")}</th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingUninvoicedColTaxId")}</th>
                          <th className="py-2 pr-4 text-right font-medium">{t("invoicingColTotal")}</th>
                          <th className="py-2 pr-4 font-medium">{t("invoicingColSale")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {items.map((item) => (
                          <UninvoicedRow
                            key={item.ticket_sale_id}
                            item={item}
                            locale={locale}
                            selected={selected.has(item.ticket_sale_id)}
                            disabled={backfilling}
                            onToggle={(checked) => toggleOne(item.ticket_sale_id, checked)}
                          />
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}

                {pagination && pagination.total_pages > 1 ? (
                  <div className="mt-4 flex items-center justify-between">
                    <p className="text-sm text-muted-foreground">
                      {t("invoicingUninvoicedPageSummary", {
                        page: formatNumber(pagination.page, locale),
                        pages: formatNumber(pagination.total_pages, locale),
                        total: pagination.total,
                      })}
                    </p>
                    <div className="flex gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={pagination.page <= 1 || backfilling}
                        onClick={() => setPage((current) => Math.max(1, current - 1))}
                      >
                        {t("previousPage")}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={pagination.page >= pagination.total_pages || backfilling}
                        onClick={() => setPage((current) => current + 1)}
                      >
                        {t("nextPage")}
                      </Button>
                    </div>
                  </div>
                ) : null}
              </CardContent>
            </Card>
          )}
        </>
      )}

      <Dialog open={confirming} onOpenChange={(open) => (backfilling ? undefined : setConfirming(open))}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("invoicingUninvoicedConfirmTitle")}</DialogTitle>
            <DialogDescription>
              {t("invoicingUninvoicedConfirmBody", { count: selectedItems.length, total: selectedTotals })}
            </DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            <li>{t("invoicingUninvoicedConfirmDatedToday")}</li>
            <li>{t("invoicingUninvoicedConfirmMail")}</li>
          </ul>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={backfilling} onClick={() => setConfirming(false)}>
              {t("invoicingUninvoicedConfirmCancel")}
            </Button>
            <Button type="button" disabled={backfilling || selectedItems.length === 0} onClick={() => void backfill()}>
              {backfilling ? t("invoicingUninvoicedBackfilling") : t("invoicingUninvoicedInvoiceSelected")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
