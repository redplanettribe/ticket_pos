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
  Textarea,
  cn,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, parsePriceToCents } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDate, formatMoney } from "@/lib/format";
import {
  type OperatorPayoutRequestDetail,
  declineOperatorPayoutRequest,
  fetchOperatorPayoutRequest,
  fulfilOperatorPayoutRequest,
  markOperatorPayoutRequestFailed,
  markOperatorPayoutRequestProcessing,
} from "@/lib/operator-api";
import {
  RESOLUTION_REASON_MAX_LENGTH,
  TRANSFER_REFERENCE_MAX_LENGTH,
  canDecline,
  canFulfil,
  canMarkFailed,
  canMarkProcessing,
  daysWaiting,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  isOutstanding,
  resolutionNotice,
  resolutionReasonProblem,
} from "@/lib/payout-requests";
import { exceedsWithdrawableBalance, todayISODate } from "@/lib/payouts";

import { usePayoutRequestStatusName } from "../../../payout-request-status";

// One payout request, with everything needed to execute the transfer (#176) and
// the four ways to answer it (#177, #186, ADR 0026 and its amendment).
//
// This is the ONE page on the platform that shows a whole bank account number,
// and it shows exactly one: an operator who opened it is about to retype that
// number into a banking app. Everything else — the queue, the organization's
// detail page — masks it.
//
// The two payable balances are the reason the page exists rather than a tooltip
// on the queue. The figure on the request is what the organization could have
// asked for when it asked; the figure beside it is what it could ask for now.
// The cap was checked once, at request time, and deliberately never again: the
// operator standing at the bank is the party who decides what to do about a
// number that has moved, and this page's whole job is to put both in front of
// them.
//
// ANSWERING HAPPENS AFTER THE TRANSFER, NEVER BEFORE IT. There is no `approved`
// state and no button that promises anything: the operator wires the money by
// hand, as they always have, and then records it here.
//
// What that recording IS split in two once PayPhone entered the picture (#186,
// ADR 0026 amendment). A transfer that settles instantly is still recorded in
// one step — that path is untouched, and a state describing uncertainty must not
// become a ritual an operator clicks through. A transfer they cannot yet confirm
// is marked PROCESSING, which writes no Payout at all, and is answered later:
// fulfilled when the money lands, or marked FAILED when the bank sends it back.
//
// Which of the four answers this page offers is decided one question at a time,
// by four predicates in lib/payout-requests.ts rather than by one "is it
// outstanding?" — see the note where they are used. `processing` is outstanding
// AND undeclinable, and a single gate cannot say both.
//
// The fulfilment amount is PRE-FILLED from the request, which is a deliberate
// departure from ADR 0019's rule against pre-filling a money field. The
// distinction is whose number it is: a refund amount is an assertion only the
// operator can make, while a payout amount is a figure the organization already
// stated and the operator agreed to by transferring it. It stays editable, and
// any divergence is spelled out beneath the field as it is typed.

/** One labelled fact from the snapshot. Definition lists read better than a table for six fields. */
function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className={cn("font-medium", mono && "font-mono")}>{value}</p>
    </div>
  );
}

/**
 * The `payouts` catalog key each bank account type is named with.
 *
 * READ FROM `payouts` AND NOT COINED HERE, because it is the same account type
 * the organizer chose on their own Payout Profile and the operator is about to
 * pay into: two words for one field is exactly what ADR 0041 exists to stop. The
 * words are Spanish in both catalogs — *Ahorros*, *Corriente* — for the same
 * reason the Tax ID Types are: they name what the receiving bank's own form
 * says, which is a fact about the world rather than a translation gap.
 *
 * A type the API adds later falls back to its raw value, the way an unknown
 * status and an unknown error code do.
 */
const ACCOUNT_TYPE_KEYS = {
  ahorros: "accountTypeAhorros",
  corriente: "accountTypeCorriente",
} as const;

export function OperatorPayoutRequestClient({ requestId }: { requestId: string }) {
  const t = useTranslations("operator");
  // `payouts` owns the bank-detail vocabulary and the resolution sentences, and
  // is read here rather than copied: the operator's screen and the organizer's
  // must name one field with one word (messages/README.md).
  const tPayouts = useTranslations("payouts");
  const statusLabel = usePayoutRequestStatusName();
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [detail, setDetail] = useState<OperatorPayoutRequestDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The fulfilment form. `amount` is seeded from the request the moment it
  // loads — see the effect below — and stays the operator's from then on.
  const [amount, setAmount] = useState("");
  const [paidAt, setPaidAt] = useState(() => todayISODate());
  const [note, setNote] = useState("");
  const [amountError, setAmountError] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // Set only while the operator is being asked to confirm an over-balance
  // amount, at the same weight as the direct record-payout confirmation
  // (ADR 0026). Carrying the parsed cents avoids re-parsing the field behind
  // them.
  const [pendingAmountCents, setPendingAmountCents] = useState<number | null>(null);
  // Set while the operator is being asked to confirm a decline. A decline is
  // final and its reason reaches the organizer, so it gets a second look.
  const [confirmingDecline, setConfirmingDecline] = useState(false);
  // The transfer an operator has submitted but cannot confirm: whatever the bank
  // handed back, which is often nothing at all (#186).
  const [transferReference, setTransferReference] = useState("");
  const [confirmingProcessing, setConfirmingProcessing] = useState(false);
  // The bank's rejection, and why. Its own field rather than the decline's,
  // because the two reach the organizer as different news and sharing a textarea
  // would let a half-typed decline be submitted as a failure.
  const [failureReason, setFailureReason] = useState("");
  const [failureReasonError, setFailureReasonError] = useState<string | null>(null);
  const [confirmingFailure, setConfirmingFailure] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setDetail(await fetchOperatorPayoutRequest(requestId));
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("requestNotFound"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [requestId]);

  useEffect(() => {
    void load();
  }, [load]);

  // THE PRE-FILL (ADR 0026, departing from ADR 0019). The amount the
  // organization asked for, as the amount field's starting value.
  //
  // Keyed on the ASKED figure alone, and not on the detail object, so the
  // re-read that follows a submission cannot throw away what the operator typed.
  // The asked figure never moves — a request cannot be edited, and fulfilment
  // leaves it recording what was asked rather than what was paid — so this runs
  // once per request. Pre-filling a number somebody else committed to is
  // defensible; overwriting one the operator entered is not.
  const requestedCents = detail?.request.amount_cents;
  useEffect(() => {
    if (requestedCents !== undefined) {
      setAmount(fulfilmentAmountDefault(requestedCents));
    }
  }, [requestedCents]);

  async function submitFulfilment(amountCents: number) {
    setSubmitting(true);
    try {
      const settledIn = detail?.organization.currency ?? "USD";
      const result = await fulfilOperatorPayoutRequest(requestId, {
        amount_cents: amountCents,
        paid_at: paidAt,
        ...(note.trim() ? { note: note.trim() } : {}),
      });
      setPendingAmountCents(null);
      setNote("");
      toast.success(
        t("fulfilled", { amount: formatMoney(result.payout.amount_cents, settledIn, locale) }),
      );
      // Re-read rather than patch locally: the request's new state, its
      // payout_id and both balances are the server's arithmetic.
      await load();
    } catch (submitError) {
      // THE REFUSAL IS THE MITIGATION on the one that matters: a lost
      // compare-and-swap wrote NOTHING, and an operator who also transferred the
      // money is told here — and only here — to record the Payout directly
      // (ADR 0026). PAYOUT_REQUEST_ALREADY_RESOLVED is catalogued so that
      // instruction reaches a Spanish-reading operator too, naming the colleague
      // who got there first from the API's own `details`; a payload that has
      // stopped carrying the name falls back to the API's English rather than
      // printing a hole (ADR 0023).
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("fulfilFailed"),
      );
      setPendingAmountCents(null);
      await load();
    } finally {
      setSubmitting(false);
    }
  }

  function handleFulfil(event: React.FormEvent) {
    event.preventDefault();
    if (!detail) {
      return;
    }
    const amountCents = parsePriceToCents(amount);
    if (amountCents === null || amountCents <= 0) {
      setAmountError(t("fulfilAmountRequired"));
      return;
    }
    if (!paidAt) {
      setAmountError(null);
      toast.error(t("paidAtRequired"));
      return;
    }
    setAmountError(null);
    // Over the withdrawable balance is allowed — the money has already moved —
    // but it gets a second look before it is written, at the same weight as the
    // direct path's confirmation (ADR 0015, ADR 0026). A divergence from what
    // was ASKED is not confirmed, only explained: transferring less is an
    // ordinary partial fulfilment.
    if (exceedsWithdrawableBalance(amountCents, detail.withdrawable_balance_cents)) {
      setPendingAmountCents(amountCents);
      return;
    }
    void submitFulfilment(amountCents);
  }

  async function submitDecline() {
    setSubmitting(true);
    try {
      await declineOperatorPayoutRequest(requestId, reason.trim());
      setConfirmingDecline(false);
      toast.success(t("declined"));
      await load();
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("declineFailed"),
      );
      setConfirmingDecline(false);
      await load();
    } finally {
      setSubmitting(false);
    }
  }

  function handleDecline(event: React.FormEvent) {
    event.preventDefault();
    const problem = resolutionReasonProblem(reason);
    // The two answers that carry a reason ask for it differently, and that
    // difference lives here — in the words each form uses — rather than in the
    // module that decides whether the box is acceptable.
    setReasonError(
      problem === "missing"
        ? t("declineReasonRequired")
        : problem === "too_long"
          ? t("reasonTooLong", { max: RESOLUTION_REASON_MAX_LENGTH })
          : null,
    );
    if (problem) {
      return;
    }
    setConfirmingDecline(true);
  }

  // Marking the transfer submitted. It writes NO Payout — that is the whole
  // reason the state exists — and the request stays outstanding, so the queue
  // and the badge keep counting it.
  async function submitProcessing() {
    setSubmitting(true);
    try {
      const trimmed = transferReference.trim();
      await markOperatorPayoutRequestProcessing(requestId, trimmed || undefined);
      setConfirmingProcessing(false);
      toast.success(t("markedProcessing"));
      await load();
    } catch (submitError) {
      // The one that matters here names the colleague who already submitted a
      // transfer, which is the difference between "try again" and "do not send
      // that money" — so PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED is
      // catalogued with that name as an argument.
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("markProcessingFailed"),
      );
      setConfirmingProcessing(false);
      await load();
    } finally {
      setSubmitting(false);
    }
  }

  // Marking the transfer failed. Nothing in the ledger is undone because nothing
  // was ever written to it, and the request ends here: the organizer corrects
  // their payout profile and asks again, because this request's copy of it is a
  // frozen snapshot.
  async function submitFailure() {
    setSubmitting(true);
    try {
      await markOperatorPayoutRequestFailed(requestId, failureReason.trim());
      setConfirmingFailure(false);
      toast.success(t("markedFailed"));
      await load();
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("markFailedFailed"),
      );
      setConfirmingFailure(false);
      await load();
    } finally {
      setSubmitting(false);
    }
  }

  function handleMarkProcessing(event: React.FormEvent) {
    event.preventDefault();
    // The reference is optional and blank is ordinary, so the only thing that can
    // be wrong with it is length — and the input's maxLength already prevents
    // that. The confirmation is the real check, and it is here because pressing
    // this button is a claim that money has left the platform's bank.
    setConfirmingProcessing(true);
  }

  function handleMarkFailed(event: React.FormEvent) {
    event.preventDefault();
    const problem = resolutionReasonProblem(failureReason);
    setFailureReasonError(
      problem === "missing"
        ? t("failureReasonRequired")
        : problem === "too_long"
          ? t("reasonTooLong", { max: RESOLUTION_REASON_MAX_LENGTH })
          : null,
    );
    if (problem) {
      return;
    }
    setConfirmingFailure(true);
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("requestLoading")}</p>;
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
        <AlertTitle>{t("requestLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("requestNotFound")}</AlertDescription>
      </Alert>
    );
  }

  const { request, organization } = detail;
  const currency = organization.currency;
  const profile = request.payout_profile;
  const snapshotPayable = request.payable_balance_cents;
  const livePayable = detail.payable_balance_cents;
  // OUTSTANDING, through the one predicate that says what that means — not a
  // second copy of it written out here. It drives what this page SAYS about the
  // ask: waiting-for-N-days rather than a resolution date, and the badge's
  // weight. Both `pending` and `processing` are outstanding, and both are
  // correctly described that way.
  //
  // It no longer drives what the page OFFERS, and that split is the whole of
  // #186. "Still awaiting an answer" and "answerable in this particular way" are
  // different questions the moment `processing` exists: a request whose transfer
  // has been submitted but not confirmed is
  // outstanding and undeclinable, and a single gate cannot say both. So the four
  // answers are asked for one at a time, each against the transition it enables,
  // and each mirroring the compare-and-swap guarding that transition on the
  // server. A gate written against `outstanding` here would offer a decline the
  // API refuses — a button whose only possible outcome is an error.
  const outstanding = isOutstanding(request.status);
  const offerFulfilment = canFulfil(request.status);
  const offerDecline = canDecline(request.status);
  const offerProcessing = canMarkProcessing(request.status);
  const offerFailure = canMarkFailed(request.status);
  const processing = request.status === "processing";
  // The Organization's currency and the platform's clock, with the reader's
  // marks. A Payout Request belongs to no Event and so to no Event's timezone.
  const money = (cents: number) => formatMoney(cents, currency, locale);
  const day = (value: string | null | undefined) => formatDate(value, PLATFORM_TIME_ZONE, locale);
  const typedCents = parsePriceToCents(amount);
  const divergence = fulfilmentDivergence(typedCents, request.amount_cents);
  const notice = resolutionNotice(request.status, request.resolution_reason);
  const accountTypeKey =
    ACCOUNT_TYPE_KEYS[profile.account_type as keyof typeof ACCOUNT_TYPE_KEYS] ?? null;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbPayoutRequests"), href: "/operator/payout-requests" },
          { label: organization.name },
        ]}
      />

      <PageHeader
        title={t("requestHeader", {
          amount: money(request.amount_cents),
          organization: organization.name,
        })}
        description={
          outstanding
            ? t("requestHeaderWaiting", {
                who: request.requested_by,
                days: daysWaiting(request.requested_at),
              })
            : t("requestHeaderAnswered", {
                who: request.requested_by,
                date: day(request.requested_at) ?? "",
              })
        }
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle>{t("askTitle")}</CardTitle>
            <CardDescription>{t("askDescription")}</CardDescription>
          </div>
          <Badge variant={outstanding ? "default" : "secondary"} className="w-fit">
            {statusLabel(request.status)}
          </Badge>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-6 sm:grid-cols-3">
            <div>
              <p className="text-sm text-muted-foreground">{t("askRequested")}</p>
              <p className="text-2xl font-semibold tabular-nums">{money(request.amount_cents)}</p>
            </div>
            {/*
              The pair the whole screen is for. The snapshot cannot move; the
              live figure can and does, and the difference between them is the
              judgement the cap deliberately leaves to the operator (ADR 0026).
            */}
            <div>
              <p className="text-sm text-muted-foreground">{t("askPayableWhenAsked")}</p>
              <p
                className={cn(
                  "text-2xl font-semibold tabular-nums",
                  snapshotPayable < 0 && "text-destructive",
                )}
              >
                {money(snapshotPayable)}
              </p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{t("askPayableNow")}</p>
              <p
                className={cn(
                  "text-2xl font-semibold tabular-nums",
                  livePayable < 0 && "text-destructive",
                )}
              >
                {money(livePayable)}
              </p>
            </div>
          </div>

          <p className="text-sm text-muted-foreground">
            {request.amount_cents > livePayable
              ? t("askAbovePayableNow", { payable: money(livePayable) })
              : t("askWithinCleared", { balance: money(detail.withdrawable_balance_cents) })}
          </p>

          {request.note ? (
            <div>
              <p className="text-sm text-muted-foreground">{t("noteFromOrganizer")}</p>
              <p>{request.note}</p>
            </div>
          ) : null}

          {/*
            A decline and a failure both carry a reason, in the same column, and
            the asker is shown it — but they are not the same news and must not
            be labelled as though they were. A decline is a judgement a person
            made; a failure is a bank sending the money back, with no judgement
            in it at all (ADR 0026 amendment). The two sentences are the
            organizer's own, read from `payouts`, so the operator sees exactly
            what the Organization will.
          */}
          {notice ? (
            <p className="text-sm text-destructive">
              {notice.kind === "declined"
                ? tPayouts("resolutionDeclined", { reason: notice.reason })
                : tPayouts("resolutionFailed", { reason: notice.reason })}
            </p>
          ) : null}
          {request.resolved_by ? (
            <p className="text-sm text-muted-foreground">
              {request.resolved_at
                ? t("answeredByOn", {
                    who: request.resolved_by,
                    date: day(request.resolved_at) ?? "",
                  })
                : t("answeredBy", { who: request.resolved_by })}
            </p>
          ) : null}

          {/*
            THE TRANSFER, once somebody has submitted one (#186). It survives
            fulfilment and failure alike, so it is shown whenever it exists
            rather than only while the request is processing: an operator reading
            a paid request six months later still wants to know who sent the
            money and what the bank called it.

            transfer_submitted_by is deliberately NOT resolved_by above. The
            colleague who submits and the colleague who confirms may be different
            people days apart, which is exactly why a three-day-old processing
            request needs to name the first of them.
          */}
          {request.transfer_submitted_at ? (
            <div className="rounded-md border p-3 text-sm">
              <p className="font-medium">
                {t("transferSubmitted", {
                  who: request.transfer_submitted_by ?? "",
                  date: day(request.transfer_submitted_at) ?? "",
                })}
              </p>
              <p className="text-muted-foreground">
                {request.transfer_reference
                  ? t("transferAgeWithReference", {
                      days: daysWaiting(request.transfer_submitted_at),
                      reference: request.transfer_reference,
                    })
                  : t("transferAgeNoReference", {
                      days: daysWaiting(request.transfer_submitted_at),
                    })}
              </p>
              {/*
                THE 72-HOUR FLAG, and it is the server's answer rather than this
                page's arithmetic: transfer_stale is computed at read time
                against the API's clock. Nothing acts on it — there is no
                reconciler and no timeout, because there is no API to ask what
                the bank did — so a human reading this line is the entire
                backstop for a transfer that quietly died.
              */}
              {request.transfer_stale ? (
                <p className="mt-2 font-medium text-destructive">{t("transferStaleDetail")}</p>
              ) : processing ? (
                <p className="mt-2 text-muted-foreground">{t("transferWaitingOnBank")}</p>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("whereToPayTitle")}</CardTitle>
          <CardDescription>{t("whereToPayDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Detail label={tPayouts("fieldBank")} value={profile.bank_name} />
          <Detail
            label={tPayouts("fieldAccountType")}
            value={accountTypeKey ? tPayouts(accountTypeKey) : profile.account_type}
          />
          <Detail label={tPayouts("fieldAccountNumber")} value={profile.account_number} mono />
          <Detail label={tPayouts("fieldAccountHolder")} value={profile.account_holder_name} />
          {/*
            The Tax ID's TYPE is its label, and its two words are Spanish in both
            catalogs because they name Ecuadorian documents a person physically
            holds — a fact about the world rather than a translation gap.
          */}
          <Detail
            label={
              profile.tax_id_type === "ruc"
                ? tPayouts("taxIdTypeRuc")
                : tPayouts("taxIdTypeCedula")
            }
            value={profile.tax_id_number}
            mono
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("requestOrganizationTitle")}</CardTitle>
          <CardDescription>{t("requestOrganizationDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Link
            href={`/operator/organizations/${organization.id}`}
            className="font-medium hover:underline"
          >
            {organization.name}
          </Link>
          <p className="text-sm text-muted-foreground">
            {t.rich("requestOrganizationBalance", {
              slug: organization.slug,
              currency,
              balance: money(detail.withdrawable_balance_cents),
              value: (chunks) => (
                <span
                  className={cn(
                    "tabular-nums",
                    detail.withdrawable_balance_cents < 0 && "text-destructive",
                  )}
                >
                  {chunks}
                </span>
              ),
            })}
          </p>
        </CardContent>
      </Card>

      {/*
        ANSWERING THE ASK (#177, #186, ADR 0026 and its amendment). Shown only
        while the request is outstanding: all four end states are final, and a
        form offering to answer an answered request would be offering something
        the API refuses.

        FOUR ANSWERS NOW, and which are offered depends on which state the
        request is in — see the predicates above. There is still no "approve":
        the operator moves the money at the bank first and records what they did
        second, and `processing` records a transfer that HAS been submitted
        rather than one somebody intends to make.
      */}
      {outstanding ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("answerTitle")}</CardTitle>
            <CardDescription>
              {processing ? t("answerDescriptionProcessing") : t("answerDescriptionPending")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-8">
            {/*
              FULFILMENT IS REACHABLE FROM BOTH OUTSTANDING STATES, which is the
              acceptance criterion this gate exists for. From `pending` it is the
              instant transfer recorded in one step, unchanged; from `processing`
              it is the submitted transfer confirmed to have landed. Same form,
              same Payout, same compare-and-swap — the state it starts from is
              not the ledger's business.
            */}
            {offerFulfilment ? (
              <form className="space-y-4" onSubmit={handleFulfil}>
                <div className="grid gap-4 sm:grid-cols-[1fr_1fr_2fr_auto] sm:items-end">
                  {/*
                    PRE-FILLED with what was asked, and editable. A payout amount
                    is a figure the organization already stated and the operator
                    agreed to by transferring it, which is what separates it from
                    the refund amount ADR 0019 refused to pre-fill.
                  */}
                  <FormField
                    id="fulfil-amount"
                    label={t("fulfilAmountLabel", { currency })}
                    error={amountError}
                  >
                    <Input
                      value={amount}
                      onChange={(event) => setAmount(event.target.value)}
                      inputMode="decimal"
                      placeholder="0.00"
                      disabled={submitting}
                    />
                  </FormField>
                  <FormField id="fulfil-paid-at" label={t("paidOnLabel")}>
                    <Input
                      type="date"
                      value={paidAt}
                      onChange={(event) => setPaidAt(event.target.value)}
                      disabled={submitting}
                    />
                  </FormField>
                  <FormField id="fulfil-note" label={t("noteLabel")}>
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
                </div>
                {/*
                  Transferring less than was asked is an ordinary partial
                  fulfilment and needs no model of its own: what is recorded is
                  what moved, the request keeps what was asked, and the difference
                  is visible forever. Saying so here is cheaper than letting an
                  operator discover it afterwards. The direction is the module's
                  and the difference is drawn here, in the Organization's
                  currency, which is the one thing that module cannot do.
                */}
                {divergence ? (
                  <p className="text-sm text-muted-foreground">
                    {divergence.direction === "short"
                      ? t("fulfilmentShort", { amount: money(divergence.differenceCents) })
                      : t("fulfilmentOver", { amount: money(divergence.differenceCents) })}
                  </p>
                ) : null}
              </form>
            ) : null}

            {/*
              MARKING THE TRANSFER SUBMITTED (#186). Offered only from `pending`:
              a request whose transfer has already been submitted must not offer a
              second operator the chance to submit another.

              This writes NO Payout, which the copy says outright, because an
              operator who believes this records the money is an operator who
              will not come back to fulfil it. The reference is optional and
              labelled so — PayPhone does not always hand one back, and a
              required field an operator cannot fill is a field they type "-"
              into.
            */}
            {offerProcessing ? (
              <form className="space-y-4 border-t pt-6" onSubmit={handleMarkProcessing}>
                <div className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end">
                  <FormField
                    id="transfer-reference"
                    label={t("transferReferenceLabel")}
                    description={t("transferReferenceDescription")}
                  >
                    <Input
                      value={transferReference}
                      onChange={(event) => setTransferReference(event.target.value)}
                      maxLength={TRANSFER_REFERENCE_MAX_LENGTH}
                      placeholder={t("transferReferencePlaceholder")}
                      disabled={submitting}
                    />
                  </FormField>
                  <Button type="submit" variant="outline" disabled={submitting}>
                    {t("markProcessing")}
                  </Button>
                </div>
              </form>
            ) : null}

            {/*
              THE BANK SENT IT BACK (#186). Offered only from `processing`: a
              transfer nobody submitted cannot have bounced, and the API refuses
              it from `pending` for the same reason.

              It is also the correction for a mis-click into processing — an
              operator who pressed the wrong button marks it failed with a reason
              saying so, rather than being stuck.
            */}
            {offerFailure ? (
              <form className="space-y-4 border-t pt-6" onSubmit={handleMarkFailed}>
                <FormField
                  id="failure-reason"
                  label={t("failureReasonLabel")}
                  error={failureReasonError}
                  description={t("failureReasonDescription")}
                >
                  <Textarea
                    value={failureReason}
                    onChange={(event) => setFailureReason(event.target.value)}
                    maxLength={RESOLUTION_REASON_MAX_LENGTH}
                    rows={3}
                    placeholder={t("failureReasonPlaceholder")}
                    disabled={submitting}
                  />
                </FormField>
                <Button type="submit" variant="outline" disabled={submitting}>
                  {t("markFailed")}
                </Button>
              </form>
            ) : null}

            {/*
              DECLINING IS `pending` ONLY, and this is the gate that had to stop
              being isOutstanding. The platform may not refuse an ask a bank is
              already acting on: cancelling from either side would leave a
              confirmed transfer with nothing to attach it to (ADR 0026
              amendment). An operator who entered processing by mistake marks it
              failed above.
            */}
            {offerDecline ? (
              <form className="space-y-4 border-t pt-6" onSubmit={handleDecline}>
                <FormField
                  id="decline-reason"
                  label={t("declineReasonLabel")}
                  error={reasonError}
                  description={t("declineReasonDescription")}
                >
                  <Textarea
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    maxLength={RESOLUTION_REASON_MAX_LENGTH}
                    rows={3}
                    placeholder={t("declineReasonPlaceholder")}
                    disabled={submitting}
                  />
                </FormField>
                <Button type="submit" variant="outline" disabled={submitting}>
                  {t("decline")}
                </Button>
              </form>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {/*
        The over-balance confirmation, at the same weight as the direct
        record-payout path's (ADR 0026). Recording is never refused for
        exceeding the balance — the money has already moved — but it is worth a
        second look.
      */}
      <Dialog
        open={pendingAmountCents !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingAmountCents(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("exceedsDialogTitle")}</DialogTitle>
            <DialogDescription>
              {pendingAmountCents === null
                ? ""
                : t("exceedsDialogBody", {
                    amount: money(pendingAmountCents),
                    balance: money(detail.withdrawable_balance_cents),
                  })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPendingAmountCents(null)}
              disabled={submitting}
            >
              {t("goBack")}
            </Button>
            <Button
              onClick={() => {
                if (pendingAmountCents !== null) {
                  void submitFulfilment(pendingAmountCents);
                }
              }}
              disabled={submitting}
            >
              {submitting ? t("recording") : t("recordItAnyway")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/*
        Marking a transfer processing is a claim that money has already left the
        platform's bank, and it takes the ask off the table for both parties —
        the organization can no longer cancel it. That is worth a second look at
        the same weight as the other two, even though it writes nothing.
      */}
      <Dialog
        open={confirmingProcessing}
        onOpenChange={(open) => !open && setConfirmingProcessing(false)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("processingDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("processingDialogBody", { organization: organization.name })}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            {transferReference.trim()
              ? t("processingDialogReference", { reference: transferReference.trim() })
              : t("processingDialogNoReference")}
          </p>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmingProcessing(false)}
              disabled={submitting}
            >
              {t("goBack")}
            </Button>
            <Button onClick={() => void submitProcessing()} disabled={submitting}>
              {submitting ? t("marking") : t("processingDialogConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/*
        A failure is final and the reason is the whole of the news the organizer
        acts on. It cannot be reopened — the bank details on this request are a
        frozen snapshot, so the fix lives on their payout profile and the next
        step is a fresh ask.
      */}
      <Dialog open={confirmingFailure} onOpenChange={(open) => !open && setConfirmingFailure(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("failureDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("failureDialogBody", { organization: organization.name })}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm">{failureReason.trim()}</p>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmingFailure(false)}
              disabled={submitting}
            >
              {t("goBack")}
            </Button>
            <Button variant="destructive" onClick={() => void submitFailure()} disabled={submitting}>
              {submitting ? t("marking") : t("markFailed")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* A decline is final and its reason reaches the organizer. */}
      <Dialog open={confirmingDecline} onOpenChange={(open) => !open && setConfirmingDecline(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("declineDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("declineDialogBody", { organization: organization.name })}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm">{reason.trim()}</p>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmingDecline(false)}
              disabled={submitting}
            >
              {t("goBack")}
            </Button>
            <Button variant="destructive" onClick={() => void submitDecline()} disabled={submitting}>
              {submitting ? t("declining") : t("decline")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
