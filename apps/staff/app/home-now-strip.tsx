"use client";

import Link from "next/link";
import type { ReactNode } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";

import { type AppLocale, formatDateTime, formatMoney, formatNumber, staffIntlLocale } from "@/lib/format";
import { type HomeNow, nextEventTimeZone, timeUntil } from "@/lib/home-now";

/**
 * One column on a phone, two from sm and four from lg: the strip's cards
 * stack rather than squeeze.
 */
export const HOME_NOW_GRID = "grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4";

export type HomeNowStripProps = {
  home: HomeNow;
  /** The Active Member's role token; an Org Admin is pointed to creating the first Event. */
  role: string | null;
  /** The instant the page was read at, in ms, so server and client agree on "in 3 days". */
  now: number;
};

function StripCard({
  title,
  badge,
  children,
}: {
  title: string;
  badge?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 space-y-0">
        <CardTitle className="text-base">{title}</CardTitle>
        {badge}
      </CardHeader>
      <CardContent className="space-y-3">{children}</CardContent>
    </Card>
  );
}

/** How long until a start, in the reader's language: "in 3 days", "dentro de 3 días". */
function startsIn(startsAt: string, now: number, locale: AppLocale): string {
  const until = timeUntil(startsAt, now);
  return new Intl.RelativeTimeFormat(staffIntlLocale(locale), { numeric: "always" }).format(until.value, until.unit);
}

/**
 * The top of Home: what stands now across the Organization (#859, ADR 0081).
 *
 * It draws what the API hands it and decides nothing about money: a Takings or
 * Payable Balance the API withheld is absent from the payload, and absent here,
 * never drawn as zero.
 */
export function HomeNowStrip({ home, role, now }: HomeNowStripProps) {
  const t = useTranslations("home");
  const locale = toAppLocale(useLocale());

  if (!home.has_events) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("emptyTitle")}</CardTitle>
          <CardDescription>
            {role === "org_admin" ? t("emptyDescriptionOrgAdmin") : t("emptyDescription")}
          </CardDescription>
        </CardHeader>
        {role === "org_admin" ? (
          <CardContent>
            <Button asChild>
              <Link href="/events/new">{t("createFirstEvent")}</Link>
            </Button>
          </CardContent>
        ) : null}
      </Card>
    );
  }

  const next = home.next_event ?? null;
  const takings = home.today_so_far.takings_cents;
  const payable = home.payable_balance_cents;

  return (
    <section aria-label={t("nowLabel")} className={HOME_NOW_GRID}>
      <StripCard title={t("activeEventsTitle")}>
        <p className="text-2xl font-semibold tabular-nums">{formatNumber(home.active_events, locale)}</p>
        <Button asChild variant="outline" size="sm">
          <Link href="/events">{t("activeEventsLink")}</Link>
        </Button>
      </StripCard>

      <StripCard
        title={t("nextEventTitle")}
        badge={next?.ongoing ? <Badge variant="success">{t("ongoing")}</Badge> : null}
      >
        {next ? (
          <>
            <Link href={`/events/${next.id}`} className="block font-medium underline-offset-4 hover:underline">
              {next.name}
            </Link>
            <p className="text-sm text-muted-foreground">
              {formatDateTime(next.starts_at, nextEventTimeZone(next, home.timezone), locale)}
            </p>
            <p className="text-sm">{t("nextEventTicketsSold", { count: next.tickets_sold })}</p>
            {next.ongoing ? null : (
              <p className="text-sm text-muted-foreground">
                {t("startsIn", { relative: startsIn(next.starts_at, now, locale) })}
              </p>
            )}
          </>
        ) : (
          <p className="text-sm text-muted-foreground">{t("noNextEvent")}</p>
        )}
      </StripCard>

      <StripCard title={t("todayTitle")} badge={<Badge variant="outline">{t("partial")}</Badge>}>
        <div>
          <p className="text-sm text-muted-foreground">{t("ticketsSold")}</p>
          <p className="text-2xl font-semibold tabular-nums">
            {formatNumber(home.today_so_far.tickets_sold, locale)}
          </p>
        </div>
        {takings === null || takings === undefined ? null : (
          <div>
            <p className="text-sm text-muted-foreground">{t("takings")}</p>
            <p className="text-2xl font-semibold tabular-nums">{formatMoney(takings, home.currency, locale)}</p>
          </div>
        )}
      </StripCard>

      {payable === null || payable === undefined ? null : (
        <StripCard title={t("payableBalanceTitle")}>
          <p className="text-2xl font-semibold tabular-nums">{formatMoney(payable, home.currency, locale)}</p>
          <Button asChild variant="outline" size="sm">
            <Link href="/payouts">{t("payableBalanceLink")}</Link>
          </Button>
        </StripCard>
      )}
    </section>
  );
}
