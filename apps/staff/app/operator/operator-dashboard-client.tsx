"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
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
import { type AppLocale, formatMoney, formatNumber } from "@/lib/format";
import {
  OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE,
  type OperatorCurrencyTotals,
  fetchOperatorOrganizations,
  fetchOperatorOutstandingQuestionReviewCount,
  fetchOperatorPendingPayoutRequestCount,
  fetchOperatorSummary,
} from "@/lib/operator-api";

/**
 * The one place platform revenue stops being "active sales only": fees kept on
 * sales an operator reversed out of band (#127). The disclosure appears only
 * when that term is non-zero — a footnote about zero is noise, and the figure
 * needs no explaining until something is standing on a voided sale.
 *
 * Fee and Fee IVA travel together everywhere, so they are disclosed as one
 * amount: what the platform kept in total.
 */
function KeptFeeNote({ totals, locale }: { totals: OperatorCurrencyTotals; locale: AppLocale }) {
  const t = useTranslations("operator");
  const keptCents = totals.kept_fee_cents + totals.kept_fee_iva_cents;
  if (keptCents === 0) {
    return null;
  }
  return (
    <p className="text-sm text-muted-foreground">
      {t("keptFeeNote", { amount: formatMoney(keptCents, totals.currency, locale) })}
    </p>
  );
}

/**
 * One currency's worth of platform revenue.
 *
 * EVERY FIGURE HERE IS IN ITS OWN CURRENCY, whichever language the operator
 * reads in. There is no exchange rate anywhere on this platform, so the card's
 * title is the currency code itself and the amounts are drawn in it; the Staff
 * Locale decides only the marks around the numbers (ADR 0041).
 */
function TotalsCard({ totals, locale }: { totals: OperatorCurrencyTotals; locale: AppLocale }) {
  const t = useTranslations("operator");
  const money = (cents: number) => formatMoney(cents, totals.currency, locale);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{totals.currency}</CardTitle>
        <CardDescription>{t("totalsDescription", { currency: totals.currency })}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-6 sm:grid-cols-3">
          <div>
            <p className="text-sm text-muted-foreground">{t("platformFees")}</p>
            <p className="text-2xl font-semibold tabular-nums">
              {money(totals.platform_fee_cents)}
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">{t("feeIva")}</p>
            <p className="text-2xl font-semibold tabular-nums">{money(totals.fee_iva_cents)}</p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">{t("totalOwed")}</p>
            <p className="text-2xl font-semibold tabular-nums">{money(totals.total_owed_cents)}</p>
          </div>
        </div>
        <KeptFeeNote totals={totals} locale={locale} />
      </CardContent>
    </Card>
  );
}

/** A count on the Overview that exists to be walked through to the work (#193). */
function CountCard({
  title,
  description,
  count,
  href,
  linkLabel,
}: {
  title: string;
  description: string;
  count: string;
  href: string;
  linkLabel: string;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-2xl font-semibold tabular-nums">{count}</p>
        <Button asChild variant="outline">
          <Link href={href}>{linkLabel}</Link>
        </Button>
      </CardContent>
    </Card>
  );
}

/**
 * The Operator Dashboard's landing page: what the platform has earned, and how
 * much work is waiting elsewhere on the surface (#193).
 *
 * Deliberately lean. The Organizations roll and the sale lookup used to sit
 * below these totals; both are destinations on the operator panel now, and the
 * counts here lead to them rather than reproducing them.
 */
export function OperatorDashboardClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [totals, setTotals] = useState<OperatorCurrencyTotals[]>([]);
  const [organizationCount, setOrganizationCount] = useState(0);
  const [pendingPayoutRequests, setPendingPayoutRequests] = useState(0);
  const [outstandingQuestionReviews, setOutstandingQuestionReviews] = useState(0);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // The organizations call is made for its pagination total alone — the roll
      // itself is read on its own page now — so it asks for the smallest slice
      // the listing will return rather than a full page it would discard (#194).
      // The Question Review count is read on its own, because it is 404
      // TICKET_QUESTIONS_UNAVAILABLE while the feature is dark (ADR 0045) and
      // a dark feature must not take the whole Overview down with it.
      const [summary, organizationsPage, payoutRequestCount, questionReviewCount] = await Promise.all([
        fetchOperatorSummary(),
        fetchOperatorOrganizations(1, OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE),
        fetchOperatorPendingPayoutRequestCount(),
        fetchOperatorOutstandingQuestionReviewCount().catch(() => null),
      ]);
      setTotals(summary.totals);
      setOrganizationCount(organizationsPage.pagination.total);
      setPendingPayoutRequests(payoutRequestCount.pending_count);
      setOutstandingQuestionReviews(questionReviewCount?.outstanding_count ?? 0);
      setForbidden(false);
    } catch (loadError) {
      // The allowlist refusal is its own answer rather than a failure to report,
      // and it is told apart by the API's CODE. It used to be told apart by
      // looking for the word "permission" inside the API's English sentence,
      // which worked only for as long as there was one language (ADR 0023).
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("loadFailed"),
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

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("title")} description={t("description")} />

      <div className="grid gap-6 sm:grid-cols-2">
        {/*
          Payout requests first: it is the one figure here that somebody is
          WAITING on, while the organization count is a standing fact.
        */}
        <CountCard
          title={t("queueCardTitle")}
          description={t("queueCardDescription")}
          count={formatNumber(pendingPayoutRequests, locale)}
          href="/operator/payout-requests"
          linkLabel={t("openTheQueue")}
        />
        {/*
          Question reviews beside the payout requests: the second thing on
          this surface that somebody is WAITING on (ADR 0056).
        */}
        <CountCard
          title={t("reviewCardTitle")}
          description={t("reviewCardDescription")}
          count={formatNumber(outstandingQuestionReviews, locale)}
          href="/operator/question-reviews"
          linkLabel={t("openTheReviews")}
        />
        <CountCard
          title={t("organizationsCardTitle")}
          description={t("organizationsCardDescription")}
          count={formatNumber(organizationCount, locale)}
          href="/operator/organizations"
          linkLabel={t("viewOrganizations")}
        />
      </div>

      {totals.length === 0 ? (
        <Card>
          <CardContent className="py-10">
            <p className="text-muted-foreground">{t("noRevenue")}</p>
          </CardContent>
        </Card>
      ) : (
        // One card per currency: there is no exchange rate anywhere on this
        // platform, so these numbers are never added together.
        totals.map((currencyTotals) => (
          <TotalsCard key={currencyTotals.currency} totals={currencyTotals} locale={locale} />
        ))
      )}
    </div>
  );
}
