"use client";

import { useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Card, CardContent, Skeleton } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatMoney, formatNumber } from "@/lib/format";
import { fetchSalesSummary, type EventSalesSummary } from "@/lib/sales-api";

import { useSalesRefreshSignal } from "./sales-refresh";

type SalesSummaryStripProps = {
  eventId: string;
};

// The Sales tab's stat strip: one honest take-home figure for the Event, above
// the list of the sales that produced it. Net Proceeds is what the Event's
// Online Sales have left the Organization once platform costs are out; the
// platform's cut is never shown as a number (ADR 0014). Rendered only for Org
// Admins and Event Owners — Event Staff get the Sales list on its own.
//
// The amount is drawn in the currency the API states it in and the count with
// the reader's marks, which is the whole of what the Staff Locale changes here:
// an organizer switching to Spanish reads the same money, spelled differently.
export function SalesSummaryStrip({ eventId }: SalesSummaryStripProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [summary, setSummary] = useState<EventSalesSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Bumped by the import section on a successful commit/undo: those change the
  // Event's sales count, so the strip re-reads alongside the list.
  const refreshSignal = useSalesRefreshSignal();

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchSalesSummary(eventId)
      .then((data) => {
        if (!cancelled) {
          setSummary(data);
        }
      })
      .catch((fetchError: unknown) => {
        if (!cancelled) {
          setError(
            (fetchError instanceof ApiError ? apiErrorMessage(errorCopy, fetchError) : null) ??
              t("summaryLoadFailed"),
          );
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, refreshSignal]);

  return (
    <Card>
      <CardContent className="grid gap-6 py-6 sm:grid-cols-2">
        <Stat
          label={t("netProceeds")}
          hint={t("netProceedsHint")}
          loading={loading}
          error={error}
          value={summary ? formatMoney(summary.net_proceeds_cents, summary.currency, locale) : null}
        />
        <Stat
          label={t("salesCount")}
          hint={t("salesCountHint")}
          loading={loading}
          error={error}
          value={summary ? formatNumber(summary.sales_count, locale) : null}
        />
      </CardContent>
    </Card>
  );
}

type StatProps = {
  label: string;
  hint: string;
  loading: boolean;
  error: string | null;
  value: string | null;
};

function Stat({ label, hint, loading, error, value }: StatProps) {
  const t = useTranslations("sales");
  return (
    <div className="space-y-1">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      {loading ? (
        <Skeleton className="h-8 w-32" />
      ) : error || value === null ? (
        <p className="text-sm text-muted-foreground">—</p>
      ) : (
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
      )}
      <p className="text-xs text-muted-foreground">
        {error ? t("summaryError", { message: error }) : hint}
      </p>
    </div>
  );
}
