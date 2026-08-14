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
import { type AppLocale, PLATFORM_TIME_ZONE, formatDate, formatMoney, formatNumber } from "@/lib/format";
import {
  type OperatorPagination,
  type OperatorPayoutRequestQueueItem,
  fetchOperatorPayoutRequests,
} from "@/lib/operator-api";
import { daysWaiting } from "@/lib/payout-requests";

import { usePayoutRequestStatusName } from "../../payout-request-status";

// The queue: who is waiting to be paid, across every organization (#176,
// ADR 0026).
//
// OLDEST FIRST, which is the one place on this platform that breaks the
// newest-first convention. This is a work queue rather than a history, and the
// oldest unanswered request is the one about to become a complaint — so the row
// that has waited longest is the row at the top, and how long it has waited is a
// column rather than a date the reader has to subtract from today.
//
// Account numbers are MASKED, and they arrive that way: the API sends
// account_number_masked and no whole number at all for a list (ADR 0026). This
// is the one screen showing every organization's bank details at once and the
// one operators screenshot into support threads, so the digits are not withheld
// from the render — they are never in the page. The whole number is one click
// away, on the request that is about to be paid.
//
// BOTH OUTSTANDING STATES QUEUE UP (#186, ADR 0026 amendment). A request whose
// transfer an operator submitted is still work — nobody has confirmed the money
// landed — so it stays here, and every row says which of the two it is. Without
// the status column a `processing` request reads as unactioned and gets
// transferred twice, which is the exact failure the state was added to prevent.
// It keeps the place its ask earned, too: the order is the age of the ASK, not
// how recently somebody touched it.
//
// The stale flag is the server's arithmetic and not this page's. It arrives as
// transfer_stale, computed at read time against the API's own clock, so the
// browser's clock — and a laptop with the wrong date — cannot make a healthy
// transfer look dead or a dead one look healthy.

function QueueRow({ item, locale }: { item: OperatorPayoutRequestQueueItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  const statusLabel = usePayoutRequestStatusName();
  const { request, organization } = item;
  const askedForAll = request.amount_cents >= request.payable_balance_cents;
  const processing = request.status === "processing";
  // Money in the ORGANIZATION's currency — the queue mixes organizations and
  // nothing here is converted — and dates on the platform's clock, both with
  // the reader's marks (ADR 0041).
  const money = (cents: number) => formatMoney(cents, organization.currency, locale);

  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link
          href={`/operator/payout-requests/${request.id}`}
          className="font-medium hover:underline"
        >
          {organization.name}
        </Link>
        <p className="font-mono text-xs text-muted-foreground">{organization.slug}</p>
      </td>
      <td className="py-3 pr-4">
        <p className="font-medium tabular-nums">{money(request.amount_cents)}</p>
        {/*
          The ask beside what could have been asked for when it was made. The
          two together are what tell an organization that asked for everything
          it had from one that asked for a slice — and the live figure, which
          may have moved since, is on the request itself.
        */}
        <p className="text-xs text-muted-foreground">
          {askedForAll
            ? t("queueAskedAll", { amount: money(request.payable_balance_cents) })
            : t("queueAskedPart", { amount: money(request.payable_balance_cents) })}
        </p>
      </td>
      <td className="py-3 pr-4">
        {/*
          HOW LONG, not WHEN: the queue is ordered oldest first because the
          oldest unanswered request is the one about to become a complaint, and
          a row saying only "12 March" makes every reader do the subtraction.
          `daysWaiting` decides the number and the catalog's plural says it.
        */}
        <p className="tabular-nums">{t("waiting", { days: daysWaiting(request.requested_at) })}</p>
        <p className="text-xs text-muted-foreground">
          {formatDate(request.requested_at, PLATFORM_TIME_ZONE, locale)}
        </p>
      </td>
      {/*
        WHAT KIND OF WORK THIS ROW IS. "Waiting" means nobody has touched it;
        "Processing" means a colleague already sent the money and is waiting on
        the bank — which is a different next action, and confusing the two is how
        the same request gets transferred twice. Both words are the catalog's own
        and are the ones the organizer reads on their own screen.

        The stale flag rides beside it rather than replacing it: a stale request
        is still processing, and what changed is only that nobody has confirmed
        it for three days. It is destructive-weight because it is the ONLY
        backstop there is — no reconciler, no timeout, nothing else will notice.
      */}
      <td className="py-3 pr-4">
        <Badge variant={processing ? "secondary" : "default"} className="w-fit">
          {statusLabel(request.status)}
        </Badge>
        {request.transfer_stale ? (
          <p className="mt-1 text-xs font-medium text-destructive">{t("transferStale")}</p>
        ) : request.transfer_submitted_at ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {t("queueTransferSent", {
              days: daysWaiting(request.transfer_submitted_at),
              who: request.transfer_submitted_by ?? "",
            })}
          </p>
        ) : null}
      </td>
      <td className="py-3 pr-4">
        <p>{request.payout_profile.bank_name}</p>
        <p className="font-mono text-xs text-muted-foreground">
          {request.payout_profile.account_number_masked}
        </p>
      </td>
      <td className="py-3 pr-4 text-sm text-muted-foreground">{request.requested_by}</td>
    </tr>
  );
}

export function OperatorPayoutRequestsClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [items, setItems] = useState<OperatorPayoutRequestQueueItem[]>([]);
  const [pagination, setPagination] = useState<OperatorPagination | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const queue = await fetchOperatorPayoutRequests(page);
      setItems(queue.data);
      setPagination(queue.pagination);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("queueLoadFailed"),
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
    return <p className="text-sm text-muted-foreground">{t("queueLoading")}</p>;
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
        <AlertTitle>{t("queueLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbPayoutRequests") },
        ]}
      />

      <PageHeader title={t("queueTitle")} description={t("queueDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("queueWaitingTitle")}</CardTitle>
          <CardDescription>{t("queueWaitingDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("queueEmpty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colOrganization")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colRequested")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colWaiting")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colStatus")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colPayingTo")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colAskedBy")}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <QueueRow key={item.request.id} item={item} locale={locale} />
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
