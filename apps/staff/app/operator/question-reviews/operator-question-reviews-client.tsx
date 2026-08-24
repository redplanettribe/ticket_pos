"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

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
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatDate, formatDateTime, formatNumber } from "@/lib/format";
import {
  type OperatorPagination,
  type OperatorQuestionReviewRow,
  fetchOperatorQuestionReviews,
} from "@/lib/operator-api";
import { daysWaiting } from "@/lib/payout-requests";

// The Question Review queue: what every organization is asking to ask, across
// the platform (#407, ADR 0056).
//
// OLDEST FIRST, on the Payout Request queue's reasoning: this is a work queue
// rather than a history, and the Review that has waited longest is the one
// whose event is nearest. The event's start sits on every row because it is
// the instant the Review lapses — a Review nobody answered before the event
// started is gone from here the next time anybody looks, with its questions
// back in draft, and no scheduler is involved (the API decides the lapse on
// every read).
//
// No question text on this screen. What was asked is read on the Review
// itself, where the verdict is given; the queue says whose, which event, how
// much, and how long.

function QueueRow({ item, locale }: { item: OperatorQuestionReviewRow; locale: AppLocale }) {
  const t = useTranslations("operator");
  const { review, organization, event } = item;
  const startsAt = event.starts_at;

  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/question-reviews/${review.id}`} className="font-medium hover:underline">
          {organization.name}
        </Link>
        <p className="font-mono text-xs text-muted-foreground">{organization.slug}</p>
      </td>
      <td className="py-3 pr-4">
        <p className="font-medium">{event.name}</p>
        {/*
          The event's start in the EVENT's timezone: it is the instant the
          Review lapses, and the organizer typed it in that zone.
        */}
        <p className="text-xs text-muted-foreground">
          {startsAt
            ? t("reviewStarts", { date: formatDateTime(startsAt, event.timezone || PLATFORM_TIME_ZONE, locale) ?? "" })
            : t("reviewNoStart")}
        </p>
      </td>
      <td className="py-3 pr-4 tabular-nums">
        {t("reviewQuestionCount", { count: review.question_count })}
      </td>
      <td className="py-3 pr-4">
        <p className="tabular-nums">{t("waiting", { days: daysWaiting(review.submitted_at) })}</p>
        <p className="text-xs text-muted-foreground">
          {formatDate(review.submitted_at, PLATFORM_TIME_ZONE, locale)}
        </p>
      </td>
      <td className="py-3 pr-4 text-sm text-muted-foreground">{review.submitted_by}</td>
    </tr>
  );
}

export function OperatorQuestionReviewsClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorQuestionReviewRow[]>([]);
  const [pagination, setPagination] = useState<OperatorPagination | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const queue = await fetchOperatorQuestionReviews(page);
      setItems(queue.data);
      setPagination(queue.pagination);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("reviewQueueLoadFailed"),
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

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("reviewQueueLoading")}</p>;
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
        <AlertTitle>{t("reviewQueueLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbQuestionReviews") },
        ]}
      />

      <PageHeader title={t("reviewQueueTitle")} description={t("reviewQueueDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("reviewQueueWaitingTitle")}</CardTitle>
          <CardDescription>{t("reviewQueueWaitingDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("reviewQueueEmpty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colOrganization")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colEvent")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colQuestions")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colWaiting")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colSubmittedBy")}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <QueueRow key={item.review.id} item={item} locale={locale} />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {pagination && pagination.total_pages > 1 ? (
            <div className="mt-4 flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                {t("queuePageSummary", {
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
                  disabled={pagination.page <= 1}
                  onClick={() => setPage((current) => Math.max(1, current - 1))}
                >
                  {t("previousPage")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pagination.page >= pagination.total_pages}
                  onClick={() => setPage((current) => current + 1)}
                >
                  {t("nextPage")}
                </Button>
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
