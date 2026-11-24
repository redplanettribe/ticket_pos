"use client";

import Link from "next/link";
import { useEffect, useId, useState } from "react";

import { Alert, AlertDescription, AlertTitle, Button, Skeleton } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import {
  fetchOrganizationTrendBreakdown,
  organizationBreakdownRequestPath,
  organizationBreakdownView,
  type OrganizationBreakdownView,
  type OrganizationTrendBreakdown,
  type OrganizationTrendBucketSelection,
} from "@/lib/organization-trends-breakdown";

/** What the panel last read, and the request it was read by: a breakdown is
 * the one on screen only while its request is the one the selection names. */
type Loaded = { path: string; breakdown: OrganizationTrendBreakdown } | { path: string; error: string };

/**
 * The Events one bucket of one figure of Organization Trends was made of
 * (#864, spec #858), drawn beneath the chart the bucket was chosen on: the
 * Platform Trends breakdown's interaction, over one Organization's Events.
 *
 * The panel reads on its own, one request per bucket chosen, and follows the
 * selection: another bucket is another request, and the Events of the bucket
 * before are not left standing under the new bucket's name. A failure is said
 * inside the panel; the chart above does not depend on it.
 */
export function OrganizationTrendBreakdownPanel({
  selection,
  figure,
  bucket,
  formatValue,
  onClose,
}: {
  selection: OrganizationTrendBucketSelection;
  /** The figure's name, as its chart's card is titled. */
  figure: string;
  /** The bucket's name, as its chart names it. */
  bucket: string;
  /** How a value of the figure is written: a count, or Takings as money. */
  formatValue: (value: number) => string;
  onClose: () => void;
}) {
  const t = useTranslations("organizationTrends");
  const errorCopy = useMessages().errors;
  const titleId = useId();
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  // Counted up by "Try again": a read that failed is asked for again under
  // the same path, which alone would not run the effect.
  const [attempt, setAttempt] = useState(0);

  const path = organizationBreakdownRequestPath(selection);
  useEffect(() => {
    let cancelled = false;
    fetchOrganizationTrendBreakdown(path)
      .then((breakdown) => {
        if (!cancelled) setLoaded({ path, breakdown });
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setLoaded({
            path,
            error: (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("breakdownFailed"),
          });
        }
      });
    return () => {
      cancelled = true;
    };
    // The path names the whole request, and the attempt asks for it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, attempt]);

  const current = loaded !== null && loaded.path === path ? loaded : null;

  return (
    <section aria-labelledby={titleId} className="space-y-3 rounded-md border bg-muted/30 p-3">
      <div className="flex items-start justify-between gap-3">
        {/* One level beneath the chart card's title. */}
        <h4 id={titleId} className="text-sm font-semibold">
          {t("breakdownTitle", { figure, bucket })}
        </h4>
        <Button type="button" variant="outline" size="sm" onClick={onClose}>
          {t("breakdownClose")}
        </Button>
      </div>
      {/* Announced as it changes: a reader who chose the bucket by keyboard
          is still on the chart above. */}
      <div aria-live="polite" className="space-y-3">
        {current === null ? (
          <div role="status" aria-label={t("breakdownLoading")}>
            <Skeleton className="h-24 w-full" />
          </div>
        ) : "error" in current ? (
          <Alert variant="destructive">
            <AlertTitle>{t("breakdownFailedTitle")}</AlertTitle>
            <AlertDescription className="space-y-2">
              <p>{current.error}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  setLoaded(null);
                  setAttempt((count) => count + 1);
                }}
              >
                {t("breakdownRetry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <OrganizationBreakdownReading
            view={organizationBreakdownView(current.breakdown)}
            formatValue={formatValue}
          />
        )}
      </div>
    </section>
  );
}

/**
 * One breakdown: the bucket's total, then its Events in the backend's order,
 * each linking to that Event. Ranked figures are numbered with their value
 * beside each Event; Events held is a plain list, each Event counting one.
 * Exported for its component test; the page draws it through the panel.
 */
export function OrganizationBreakdownReading({
  view,
  formatValue,
}: {
  view: OrganizationBreakdownView;
  formatValue: (value: number) => string;
}) {
  const t = useTranslations("organizationTrends");
  const listId = useId();
  const List = view.ranked ? "ol" : "ul";
  return (
    <>
      {view.partial ? <p className="text-xs text-muted-foreground">{t("breakdownPartial")}</p> : null}
      <p className="flex items-baseline justify-between gap-3 text-sm">
        <span className="text-muted-foreground">{t("breakdownTotal")}</span>
        <span className="font-medium tabular-nums">{formatValue(view.total)}</span>
      </p>
      {view.rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("breakdownEmpty")}</p>
      ) : (
        <div className="min-w-0 space-y-1">
          <h5 id={listId} className="text-xs font-medium text-muted-foreground">
            {t("breakdownEvents")}
          </h5>
          {/* A busy day has many Events, none of them cut: the list scrolls
              inside the panel and the chart stays in view. */}
          <List
            aria-labelledby={listId}
            className={`max-h-64 space-y-1 overflow-y-auto text-sm ${view.ranked ? "list-decimal pl-6" : ""}`}
          >
            {view.rows.map((row) => (
              <li key={row.id}>
                <div className="flex items-baseline justify-between gap-3">
                  <Link href={row.href} className="min-w-0 break-words font-medium hover:underline">
                    {row.name}
                  </Link>
                  {row.value === null ? null : <span className="shrink-0 tabular-nums">{formatValue(row.value)}</span>}
                </div>
              </li>
            ))}
          </List>
        </div>
      )}
    </>
  );
}
