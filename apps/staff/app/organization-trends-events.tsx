"use client";

import Link from "next/link";
import { useId } from "react";

import type { AppLocale } from "@ticket-pos/locale";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { formatDate, formatMoney, formatNumber } from "@/lib/format";
import type { OrganizationTrends } from "@/lib/organization-trends";
import { organizationEventTable, type OrganizationEventTableRow } from "@/lib/organization-trends-events";

const CELL = "py-2 pr-3 align-top";
const NUMBER_CELL = `${CELL} text-right tabular-nums whitespace-nowrap`;

/**
 * The Event table of Organization Trends (#865, spec #858), beneath the
 * charts: every Event held, loaded or sold in the span, one row each, adding
 * up to the totals above. Ranked by Takings when the reading carries Takings
 * (an Org Admin, an Event Owner), by Tickets Sold with no Takings column when
 * it does not (Event Staff). Each row links into that Event's Sales Trends and
 * Reach.
 *
 * On a phone the start date moves beneath the Event's name, so the figures
 * keep their columns; a table wider than its card still scrolls sideways.
 */
export function OrganizationEventTableCard({ trends, locale }: { trends: OrganizationTrends; locale: AppLocale }) {
  const t = useTranslations("organizationTrends");
  const titleId = useId();
  const table = organizationEventTable(trends);

  return (
    <Card>
      <CardHeader>
        <CardTitle id={titleId}>{t("eventsTableTitle")}</CardTitle>
        <CardDescription>
          {table.showsTakings ? t("eventsTableDescriptionTakings") : t("eventsTableDescriptionTickets")}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {table.rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("eventsTableEmpty")}</p>
        ) : (
          <div className="overflow-x-auto">
            <table aria-labelledby={titleId} className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs text-muted-foreground">
                  <th scope="col" className={`${CELL} font-medium`}>
                    {t("eventsTableEvent")}
                  </th>
                  <th scope="col" className={`${CELL} hidden font-medium sm:table-cell`}>
                    {t("eventsTableStart")}
                  </th>
                  {table.showsTakings ? (
                    <th scope="col" className={`${NUMBER_CELL} font-medium`}>
                      {t("takings")}
                    </th>
                  ) : null}
                  <th scope="col" className={`${NUMBER_CELL} font-medium`}>
                    {t("ticketsSold")}
                  </th>
                  <th scope="col" className={`${NUMBER_CELL} font-medium`}>
                    {t("pageViews")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {table.rows.map((row) => (
                  <EventRow key={row.id} row={row} currency={trends.currency} locale={locale} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function EventRow({ row, currency, locale }: { row: OrganizationEventTableRow; currency: string; locale: AppLocale }) {
  const t = useTranslations("organizationTrends");
  const start = row.startsAt === null ? null : formatDate(row.startsAt, row.timeZone, locale);
  const startText = start ?? t("eventsTableNoStart");
  return (
    <tr className="border-b last:border-b-0">
      <th scope="row" className={`${CELL} min-w-40 text-left font-normal`}>
        <span className="block break-words font-medium">{row.name}</span>
        <span className="block text-xs text-muted-foreground sm:hidden">{startText}</span>
        <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs">
          <Link
            href={row.salesTrendsHref}
            aria-label={t("eventsTableSalesTrendsFor", { event: row.name })}
            className="text-primary underline-offset-2 hover:underline"
          >
            {t("eventsTableSalesTrends")}
          </Link>
          <Link
            href={row.reachHref}
            aria-label={t("eventsTableReachFor", { event: row.name })}
            className="text-primary underline-offset-2 hover:underline"
          >
            {t("eventsTableReach")}
          </Link>
        </span>
      </th>
      <td className={`${CELL} hidden whitespace-nowrap text-muted-foreground sm:table-cell`}>{startText}</td>
      {row.takingsCents === null ? null : (
        <td className={NUMBER_CELL}>{formatMoney(row.takingsCents, currency, locale)}</td>
      )}
      <td className={NUMBER_CELL}>{formatNumber(row.ticketsSold, locale)}</td>
      <td className={NUMBER_CELL}>{formatNumber(row.pageViews, locale)}</td>
    </tr>
  );
}
