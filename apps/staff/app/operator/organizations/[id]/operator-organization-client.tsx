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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  PageHeader,
  cn,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, eventStatusKey, parsePriceToCents, statusBadgeVariant } from "@/lib/events-api";
import {
  PLATFORM_TIME_ZONE,
  formatCalendarDay,
  formatDate,
  formatDateTime,
  formatMoney,
} from "@/lib/format";
import {
  type OperatorOrganizationDetail,
  type OperatorPayoutRequestRow,
  fetchOperatorOrganization,
  recordOperatorPayout,
} from "@/lib/operator-api";
import { isOutstanding, resolutionNotice } from "@/lib/payout-requests";
import { exceedsWithdrawableBalance, outstandingPayoutRequest, todayISODate } from "@/lib/payouts";

import { usePayoutRequestStatusName } from "../../../payout-request-status";

type OperatorOrganizationClientProps = {
  organizationId: string;
};

/**
 * A Payout the operator has typed and the form has not written yet, because
 * something about it deserves a second look first.
 *
 * The two reasons travel together in one state rather than in two dialogs
 * because they can both be true at once, and an operator shown them one after
 * the other would answer the second without the first still in front of them.
 * Neither is a gate: recording a Payout is unconditional on the API side, over
 * the balance (ADR 0015) and against an outstanding request alike (ADR 0026).
 */
type PendingPayout = {
  amountCents: number;
  /** Over the Withdrawable Balance: this settlement leaves it negative. */
  exceedsBalance: boolean;
  /** The Organization's outstanding ask, when it has one (#178). */
  outstandingRequest: OperatorPayoutRequestRow | null;
};

export function OperatorOrganizationClient({ organizationId }: OperatorOrganizationClientProps) {
  const t = useTranslations("operator");
  // Three namespaces, and each is the authority on its own vocabulary. `payouts`
  // owns both balance names — *Saldo por retirar* and *Saldo pagable* — because
  // an organizer phoning a Platform Operator about their payable balance must be
  // speaking the word the operator's screen uses. `events` owns the Event status
  // vocabulary for the same reason (messages/README.md).
  const tPayouts = useTranslations("payouts");
  const tEvents = useTranslations("events");
  const statusLabel = usePayoutRequestStatusName();
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [detail, setDetail] = useState<OperatorOrganizationDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [amount, setAmount] = useState("");
  const [paidAt, setPaidAt] = useState(() => todayISODate());
  const [note, setNote] = useState("");
  const [amountError, setAmountError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // Set only while the operator is being asked to confirm; carrying the parsed
  // cents avoids re-parsing the field behind them.
  const [pending, setPending] = useState<PendingPayout | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setDetail(await fetchOperatorOrganization(organizationId));
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("organizationNotFound"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [organizationId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function submitPayout(amountCents: number) {
    setSubmitting(true);
    try {
      await recordOperatorPayout(organizationId, {
        amount_cents: amountCents,
        paid_at: paidAt,
        ...(note.trim() ? { note: note.trim() } : {}),
      });
      setAmount("");
      setNote("");
      setPending(null);
      toast.success(t("payoutRecorded"));
      // Re-read rather than patch locally: the Withdrawable Balance is the
      // server's arithmetic, and this page shows it in two places.
      await load();
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("payoutFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!detail) {
      return;
    }
    const amountCents = parsePriceToCents(amount);
    if (amountCents === null || amountCents <= 0) {
      setAmountError(t("amountNotPositive"));
      return;
    }
    if (!paidAt) {
      setAmountError(null);
      toast.error(t("paidAtRequired"));
      return;
    }
    setAmountError(null);
    // Two second looks, either of which stops the write until the operator says
    // so, and neither of which can refuse it.
    //
    // Over the balance is allowed — the money has already moved (ADR 0015).
    // Recording directly while the Organization has an outstanding ask is
    // allowed for the same reason, and warned about because the failure it
    // invites is paying the same Organization twice: the colleague who is about
    // to fulfil that request has no way to see this form (#178, ADR 0026).
    const exceedsBalance = exceedsWithdrawableBalance(
      amountCents,
      detail.withdrawable_balance_cents,
    );
    const outstanding = outstandingPayoutRequest(detail.payout_requests);
    if (exceedsBalance || outstanding) {
      setPending({ amountCents, exceedsBalance, outstandingRequest: outstanding });
      return;
    }
    void submitPayout(amountCents);
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("organizationLoading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (error || !detail) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("organizationLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("organizationNotFound")}</AlertDescription>
      </Alert>
    );
  }

  const {
    organization,
    withdrawable_balance_cents: balanceCents,
    payable_balance_cents: payableCents,
    events,
    payouts,
    payout_requests: payoutRequests,
  } = detail;
  const currency = organization.currency;
  // Money in the ORGANIZATION's currency and moments on the platform's clock,
  // both with the reader's marks. Nothing on this page belongs to an Event, so
  // there is no Event timezone to draw it in: Ecuador's is what the platform
  // states its own facts in (ADR 0041).
  const money = (cents: number) => formatMoney(cents, currency, locale);
  const day = (value: string | null | undefined) => formatDate(value, PLATFORM_TIME_ZONE, locale);
  // The ask this Organization is waiting on, if any. It survives a direct
  // Payout — nothing here auto-closes a request (ADR 0026) — so an operator who
  // records one settlement and comes back for another is warned both times.
  const outstanding = outstandingPayoutRequest(payoutRequests);

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOrganizations"), href: "/operator/organizations" },
          { label: organization.name },
        ]}
      />

      <PageHeader
        title={organization.name}
        description={t("organizationSubtitle", {
          slug: organization.slug,
          currency,
          date: day(organization.created_at) ?? "",
        })}
      />

      {/*
        Both balances, beside each other, because the operator about to transfer
        money is the one person who benefits most from knowing which part of it
        has cleared (ADR 0026). Neither figure gates anything: recording a Payout
        stays unconditional, and the confirmation below is still checked against
        the Withdrawable Balance, which is what the platform actually owes.
      */}
      <Card>
        <CardHeader>
          <CardTitle>{t("balancesTitle")}</CardTitle>
          <CardDescription>{t("balancesDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <div>
            <p className="text-sm text-muted-foreground">{tPayouts("withdrawableBalance")}</p>
            <p
              className={cn(
                "text-3xl font-semibold tabular-nums",
                balanceCents < 0 && "text-destructive",
              )}
            >
              {money(balanceCents)}
            </p>
            {balanceCents < 0 ? (
              <p className="mt-2 text-sm text-muted-foreground">{t("balanceNegative")}</p>
            ) : null}
          </div>
          <div>
            <p className="text-sm text-muted-foreground">{tPayouts("payableBalance")}</p>
            <p
              className={cn(
                "text-3xl font-semibold tabular-nums",
                payableCents < 0 && "text-destructive",
              )}
            >
              {money(payableCents)}
            </p>
            <p className="mt-2 text-sm text-muted-foreground">
              {payableCents < balanceCents
                ? t("payableBelowWithdrawable")
                : t("payableAllCleared")}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("recordPayoutTitle")}</CardTitle>
          <CardDescription>
            {t.rich("recordPayoutDescription", {
              balance: money(balanceCents),
              value: (chunks) => (
                <span className={cn("tabular-nums", balanceCents < 0 && "text-destructive")}>
                  {chunks}
                </span>
              ),
            })}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/*
            Said before the form is touched as well as on submit, because an
            operator who learns about the outstanding ask only after typing an
            amount has already decided what to do. The fulfilment route is the
            better door: it records the same Payout and answers the request in
            one transaction, which is what stops the second operator recording
            the same transfer twice (ADR 0026).
          */}
          {outstanding ? (
            <Alert variant="warning">
              <AlertTitle>{t("outstandingAlertTitle")}</AlertTitle>
              <AlertDescription className="space-y-2">
                <p>
                  {t.rich("outstandingAlertBody", {
                    organization: organization.name,
                    amount: money(outstanding.amount_cents),
                    date: day(outstanding.requested_at) ?? "",
                    value: (chunks) => (
                      <span className="font-medium tabular-nums">{chunks}</span>
                    ),
                  })}
                </p>
                <Button asChild variant="outline" size="sm">
                  <Link href={`/operator/payout-requests/${outstanding.id}`}>
                    {t("fulfilInstead")}
                  </Link>
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          <form
            className="grid gap-4 sm:grid-cols-[1fr_1fr_2fr_auto] sm:items-end"
            onSubmit={handleSubmit}
          >
            <FormField id="payout-amount" label={t("amountLabel", { currency })} error={amountError}>
              <Input
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
                inputMode="decimal"
                placeholder="0.00"
                disabled={submitting}
              />
            </FormField>
            <FormField id="payout-paid-at" label={t("paidOnLabel")}>
              <Input
                type="date"
                value={paidAt}
                onChange={(event) => setPaidAt(event.target.value)}
                disabled={submitting}
              />
            </FormField>
            <FormField id="payout-note" label={t("noteLabel")}>
              <Input
                value={note}
                onChange={(event) => setNote(event.target.value)}
                placeholder={t("notePlaceholder")}
                disabled={submitting}
              />
            </FormField>
            <Button type="submit" disabled={submitting}>
              {submitting ? t("recording") : t("recordPayout")}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("eventsTitle")}</CardTitle>
          <CardDescription>{t("eventsDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {events.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("eventsEmpty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colEvent")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colStatus")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colStarts")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colDiscoverable")}</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((event) => {
                    const statusKey = eventStatusKey(event.status);
                    return (
                      <tr key={event.id} className="border-b last:border-b-0">
                        {/* An Event's name is data and reads as coined. */}
                        <td className="py-3 pr-4 font-medium">{event.name}</td>
                        <td className="py-3 pr-4">
                          <Badge variant={statusBadgeVariant(event.status)} className="w-fit">
                            {/* A status the backend adds later shows as the
                                server named it rather than as a blank badge. */}
                            {statusKey ? tEvents(statusKey) : event.status}
                          </Badge>
                        </td>
                        {/*
                          This payload carries no Event timezone, so the schedule
                          is drawn on the platform's own clock and labelled by
                          nothing else. `formatDateTime` demands a zone and has
                          no default precisely so that this is a decision rather
                          than the reader's laptop answering.
                        */}
                        <td className="py-3 pr-4">
                          {formatDateTime(event.starts_at, PLATFORM_TIME_ZONE, locale) ??
                            tEvents("noDateSet")}
                        </td>
                        <td className="py-3 pr-4 text-muted-foreground">
                          {event.discoverable ? t("yes") : t("no")}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("payoutHistoryTitle")}</CardTitle>
          <CardDescription>{t("payoutHistoryDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {payouts.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("payoutHistoryEmpty")}</p>
          ) : (
            <div className="space-y-3">
              {payouts.map((payout) => (
                <div
                  key={payout.id}
                  className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div>
                    <p className="font-medium tabular-nums">{money(payout.amount_cents)}</p>
                    {payout.note ? (
                      <p className="text-sm text-muted-foreground">{payout.note}</p>
                    ) : null}
                  </div>
                  <div className="sm:text-right">
                    {/*
                      A calendar day, not an instant: "2026-03-01" through the
                      Date constructor is UTC midnight, which is the 28th of
                      February everywhere this platform sells.
                    */}
                    <p className="text-sm text-muted-foreground">
                      {formatCalendarDay(payout.paid_at, locale)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {payout.recorded_by
                        ? t("payoutRecordedBy", { who: payout.recorded_by })
                        : t("payoutRecordedDirectly")}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/*
        The Organization's asks, beside the payout history they belong with:
        together the two answer "has this Organization been paid recently, and
        are they asking again?" (ADR 0026). Newest first, because this is a
        history — the top-level queue is the one place that inverts it, and it
        inverts it because it is a work queue.

        Masked here as well. One Organization's details are still bank details,
        and the place to read an account number is the request that is about to
        be paid; the API sends no whole number to this page at all.
      */}
      <Card>
        <CardHeader>
          <CardTitle>{t("organizationRequestsTitle")}</CardTitle>
          <CardDescription>{t("organizationRequestsDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {payoutRequests.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("organizationRequestsEmpty")}</p>
          ) : (
            <div className="space-y-3">
              {payoutRequests.map((request) => {
                // A DECLINE AND A FAILURE ARE NOT ONE SENTENCE. This row used to
                // label every resolution reason "Declined:", including a bank
                // rejection — which tells an operator the platform judged an
                // Organization it never judged. The token says which it is and
                // the catalog says it in the words the organizer's own screen
                // and their notice email use (ADR 0026 amendment).
                const notice = resolutionNotice(request.status, request.resolution_reason);
                return (
                  <div
                    key={request.id}
                    className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-start sm:justify-between"
                  >
                    <div>
                      <p className="font-medium tabular-nums">
                        {isOutstanding(request.status) ? (
                          <Link
                            href={`/operator/payout-requests/${request.id}`}
                            className="hover:underline"
                          >
                            {money(request.amount_cents)}
                          </Link>
                        ) : (
                          money(request.amount_cents)
                        )}
                      </p>
                      {request.note ? (
                        <p className="text-sm text-muted-foreground">{request.note}</p>
                      ) : null}
                      {notice ? (
                        <p className="text-sm text-destructive">
                          {notice.kind === "declined"
                            ? tPayouts("resolutionDeclined", { reason: notice.reason })
                            : tPayouts("resolutionFailed", { reason: notice.reason })}
                        </p>
                      ) : null}
                      <p className="text-sm text-muted-foreground">
                        {tPayouts("requestPayingTo", {
                          bank: request.payout_profile.bank_name,
                          account: request.payout_profile.account_number_masked,
                        })}
                      </p>
                    </div>
                    <div className="sm:text-right">
                      <p className="text-sm font-medium">{statusLabel(request.status)}</p>
                      <p className="text-sm text-muted-foreground">
                        {t("requestAskedOnBy", {
                          date: day(request.requested_at) ?? "",
                          who: request.requested_by,
                        })}
                      </p>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/*
        The confirmation, carrying whichever of the two second looks apply. It
        is dismissible in every case and refuses nothing: the transfer already
        happened, and a form that would not write it down has not prevented
        anything, it has only stopped knowing (ADR 0019, ADR 0026).
      */}
      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPending(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {pending?.outstandingRequest
                ? t("confirmOutstandingTitle")
                : t("confirmExceedsTitle")}
            </DialogTitle>
            <DialogDescription asChild>
              <div className="space-y-3">
                {pending?.outstandingRequest ? (
                  <p>
                    {t.rich("confirmOutstandingBody", {
                      organization: organization.name,
                      requested: money(pending.outstandingRequest.amount_cents),
                      date: day(pending.outstandingRequest.requested_at) ?? "",
                      who: pending.outstandingRequest.requested_by,
                      amount: money(pending.amountCents),
                      value: (chunks) => (
                        <span className="font-medium tabular-nums">{chunks}</span>
                      ),
                    })}
                  </p>
                ) : null}
                {pending?.exceedsBalance ? (
                  <p>
                    {t("confirmExceedsBody", {
                      amount: money(pending.amountCents),
                      balance: money(balanceCents),
                      organization: organization.name,
                    })}
                  </p>
                ) : null}
              </div>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPending(null)}
              disabled={submitting}
            >
              {t("cancel")}
            </Button>
            {pending?.outstandingRequest ? (
              <Button asChild variant="secondary">
                <Link href={`/operator/payout-requests/${pending.outstandingRequest.id}`}>
                  {t("fulfilInstead")}
                </Link>
              </Button>
            ) : null}
            <Button
              type="button"
              onClick={() => {
                if (pending !== null) {
                  void submitPayout(pending.amountCents);
                }
              }}
              disabled={submitting}
            >
              {submitting ? t("recording") : t("recordAnyway")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
