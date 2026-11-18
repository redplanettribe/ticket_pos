"use client";

import Link from "next/link";
import { useEffect, useId, useState } from "react";

import type { AppLocale } from "@ticket-pos/locale";
import { Alert, AlertDescription, AlertTitle, Button, Skeleton } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatMoney, formatNumber } from "@/lib/format";
import { formatTrendBucket, type PlatformTrendGranularity } from "@/lib/platform-trends";
import {
  breakdownRequestPath,
  breakdownView,
  fetchPlatformTrendBreakdown,
  type BreakdownGroup,
  type BreakdownRow,
  type BreakdownUnit,
  type BreakdownView,
  type PlatformTrendBreakdown,
  type TrendBucketSelection,
} from "@/lib/platform-trends-breakdown";

const CAUSE_KEYS = {
  sale: "trendsBreakdownCauseSale",
  member: "trendsBreakdownCauseMember",
  both: "trendsBreakdownCauseBoth",
} as const;

/** What the panel last read, and the request it was read by: a breakdown is
 * the one on screen only while its request is the one the selection names. */
type Loaded = { path: string; breakdown: PlatformTrendBreakdown } | { path: string; error: string };

/**
 * What one bucket of one figure was made of (#822, spec #808), drawn beneath
 * the chart the bucket was chosen on.
 *
 * The panel reads on its own, one request per bucket chosen, and follows the
 * selection: another bucket of the chart is another request, and the
 * breakdown before it is not left standing under the new bucket's name. A
 * failure is said inside the panel; the chart above does not depend on it.
 *
 * No shape here names a Customer. Active Customers is three counts and new
 * Verified Customers one, and `breakdownView` never hands either a list.
 */
export function PlatformTrendBreakdownPanel({
  selection,
  figure,
  granularity,
  locale,
  onClose,
}: {
  selection: TrendBucketSelection;
  /** The figure's name, as its chart's card is titled. */
  figure: string;
  /** The reading's granularity, which says what a bucket is asked for as. */
  granularity: PlatformTrendGranularity;
  locale: AppLocale;
  onClose: () => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const titleId = useId();
  const [loaded, setLoaded] = useState<Loaded | null>(null);

  const path = breakdownRequestPath(selection, granularity);
  useEffect(() => {
    let cancelled = false;
    fetchPlatformTrendBreakdown(path)
      .then((breakdown) => {
        if (!cancelled) {
          setLoaded({ path, breakdown });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setLoaded({
            path,
            error: (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("trendsBreakdownFailed"),
          });
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  // What was read for another bucket is not this bucket's: until this one
  // arrives the panel is loading.
  const current = loaded !== null && loaded.path === path ? loaded : null;
  const bucket = formatTrendBucket({ first_day: selection.firstDay, last_day: selection.lastDay }, locale);

  return (
    <section aria-labelledby={titleId} className="space-y-3 rounded-md border bg-muted/30 p-3">
      <div className="flex items-start justify-between gap-3">
        <h3 id={titleId} className="text-sm font-semibold">
          {t("trendsBreakdownTitle", { figure, bucket })}
        </h3>
        <Button type="button" variant="outline" size="sm" onClick={onClose}>
          {t("trendsBreakdownClose")}
        </Button>
      </div>
      {/* Announced as it changes: a reader who chose the bucket by keyboard
          is still on the chart above. */}
      <div aria-live="polite" aria-busy={current === null} className="space-y-3">
        {current === null ? (
          <div role="status" aria-label={t("trendsBreakdownLoading")}>
            <Skeleton className="h-24 w-full" />
          </div>
        ) : "error" in current ? (
          <Alert variant="destructive">
            <AlertTitle>{t("trendsBreakdownFailedTitle")}</AlertTitle>
            <AlertDescription>{current.error}</AlertDescription>
          </Alert>
        ) : (
          <BreakdownReading view={breakdownView(current.breakdown)} locale={locale} />
        )}
      </div>
    </section>
  );
}

/** One breakdown, drawn in its figure's shape. */
function BreakdownReading({ view, locale }: { view: BreakdownView; locale: AppLocale }) {
  const t = useTranslations("operator");
  const count = (value: number) => formatNumber(value, locale);
  const { body } = view;
  const total = (value: number) => ({ id: "total", label: t("trendsBreakdownTotal"), value: count(value) });
  const figures =
    body.kind === "count"
      ? [total(body.total)]
      : body.kind === "customerKinds"
        ? [
            ...(body.total === null ? [] : [total(body.total)]),
            { id: "new", label: t("trendsBreakdownCustomersNew"), value: count(body.new) },
            { id: "returning", label: t("trendsBreakdownCustomersReturning"), value: count(body.returning) },
            {
              id: "returningBought",
              label: t("trendsBreakdownCustomersReturningBought"),
              value: count(body.returningBought),
            },
          ]
        : [];
  return (
    <>
      {view.partial || view.salesOnly || view.sumOfDays ? (
        <div className="space-y-1 text-xs text-muted-foreground">
          {view.partial ? <p>{t("trendsBreakdownPartial")}</p> : null}
          {view.salesOnly ? <p>{t("trendsBreakdownSalesOnly")}</p> : null}
          {view.sumOfDays ? <p>{t("trendsBreakdownSumOfDays")}</p> : null}
        </div>
      ) : null}
      {body.kind === "notRecorded" ? (
        <p className="text-sm text-muted-foreground">{t("trendsBreakdownNotRecorded")}</p>
      ) : body.kind === "empty" ? (
        <p className="text-sm text-muted-foreground">{t("trendsBreakdownEmpty")}</p>
      ) : body.kind === "count" || body.kind === "customerKinds" ? (
        // Counts, and nobody: neither figure ever lists a Customer.
        <>
          <Figures entries={figures} />
          <p className="text-xs text-muted-foreground">{t("trendsBreakdownCountsOnly")}</p>
        </>
      ) : (
        body.groups.map((group) => <BreakdownGroupLists key={group.key} group={group} unit={body.unit} locale={locale} />)
      )}
    </>
  );
}

/** Counts stated as a short list of terms: a total, the kinds of Customer. */
function Figures({ entries }: { entries: { id: string; label: string; value: string }[] }) {
  return (
    <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
      {entries.map((entry) => (
        <div key={entry.id} className="flex items-baseline justify-between gap-3">
          <dt className="text-muted-foreground">{entry.label}</dt>
          <dd className="font-medium tabular-nums">{entry.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** The contributors toward one total: its Organizations, then its Events,
 * each in the order the backend ranked them. A money group is headed by its
 * currency and no figure crosses from one group to another. */
function BreakdownGroupLists({ group, unit, locale }: { group: BreakdownGroup; unit: BreakdownUnit; locale: AppLocale }) {
  const t = useTranslations("operator");
  const spell = (value: number) =>
    unit === "money" && group.currency !== null
      ? formatMoney(value, group.currency, locale)
      : unit === "days"
        ? t("trendsBreakdownDaysActive", { days: value })
        : formatNumber(value, locale);
  // The total of days is a count of days, said as a number under its own
  // label, and every other total is in the unit of its rows.
  const total = unit === "days" ? formatNumber(group.total, locale) : spell(group.total);
  return (
    <div className="space-y-2">
      <p className="flex items-baseline justify-between gap-3 text-sm">
        <span className="text-muted-foreground">
          {group.currency === null
            ? t(unit === "days" ? "trendsBreakdownTotalDays" : "trendsBreakdownTotal")
            : t("trendsBreakdownTotalCurrency", { currency: group.currency })}
        </span>
        <span className="font-medium tabular-nums">{total}</span>
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <BreakdownList
          title={
            group.currency === null
              ? t("trendsBreakdownOrganizations")
              : t("trendsBreakdownOrganizationsCurrency", { currency: group.currency })
          }
          rows={group.organizations}
          spell={spell}
          ranked={unit !== "none"}
        />
        <BreakdownList
          title={
            group.currency === null
              ? t("trendsBreakdownEvents")
              : t("trendsBreakdownEventsCurrency", { currency: group.currency })
          }
          rows={group.events}
          spell={spell}
          ranked={unit !== "none"}
        />
      </div>
    </div>
  );
}

/**
 * One list of a breakdown. Every row links to an Organization's operator
 * page: its own, or for an Event its Organization's, named beside it, since
 * there is no operator page for an Event.
 *
 * A ranked list is numbered; a plain list (Events Published, new
 * Organizations) is not, as its order is not a ranking. A figure not broken
 * down this way hands no rows and draws nothing.
 */
function BreakdownList({
  title,
  rows,
  spell,
  ranked,
}: {
  title: string;
  rows: BreakdownRow[];
  spell: (value: number) => string;
  ranked: boolean;
}) {
  const t = useTranslations("operator");
  const titleId = useId();
  if (rows.length === 0) {
    return null;
  }
  const List = ranked ? "ol" : "ul";
  return (
    <div className="min-w-0 space-y-1">
      <h4 id={titleId} className="text-xs font-medium text-muted-foreground">
        {title}
      </h4>
      {/* A long day has many contributors, none of them cut: the list scrolls
          inside the panel and the chart stays in view. */}
      <List aria-labelledby={titleId} className={`max-h-64 space-y-1 overflow-y-auto text-sm ${ranked ? "list-decimal pl-6" : ""}`}>
        {rows.map((row) => (
          <li key={row.id}>
            <div className="flex items-baseline justify-between gap-3">
              <span className="min-w-0 break-words">
                <Link href={row.href} className="font-medium hover:underline">
                  {row.name}
                </Link>
                {row.organizationName === null ? null : (
                  <span className="text-muted-foreground"> {t("trendsBreakdownEventOf", { organization: row.organizationName })}</span>
                )}
                {row.cause === null ? null : (
                  <span className="text-muted-foreground"> {t(CAUSE_KEYS[row.cause])}</span>
                )}
              </span>
              {row.value === null ? null : <span className="shrink-0 tabular-nums">{spell(row.value)}</span>}
            </div>
          </li>
        ))}
      </List>
    </div>
  );
}
