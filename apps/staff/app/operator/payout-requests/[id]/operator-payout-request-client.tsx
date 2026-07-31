"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

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

import { formatPriceCents, parsePriceToCents } from "@/lib/events-api";
import {
  type OperatorPayoutRequestDetail,
  declineOperatorPayoutRequest,
  fetchOperatorPayoutRequest,
  fulfilOperatorPayoutRequest,
  markOperatorPayoutRequestFailed,
  markOperatorPayoutRequestProcessing,
} from "@/lib/operator-api";
import {
  DECLINE_REASON_MAX_LENGTH,
  TRANSFER_REFERENCE_MAX_LENGTH,
  canDecline,
  canFulfil,
  canMarkFailed,
  canMarkProcessing,
  declineReasonProblem,
  failureReasonProblem,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  isOutstanding,
  payoutRequestStatusLabel,
  transferSentLabel,
  waitingLabel,
} from "@/lib/payout-requests";
import { exceedsWithdrawableBalance, todayISODate } from "@/lib/payouts";

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

const ACCOUNT_TYPE_LABELS: Record<string, string> = {
  ahorros: "Ahorros (savings)",
  corriente: "Corriente (current)",
};

export function OperatorPayoutRequestClient({ requestId }: { requestId: string }) {
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
      const message = loadError instanceof Error ? loadError.message : "Failed to load payout request";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
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
        `Payout of ${formatPriceCents(result.payout.amount_cents, settledIn)} recorded, and the request is marked paid`,
      );
      // Re-read rather than patch locally: the request's new state, its
      // payout_id and both balances are the server's arithmetic.
      await load();
    } catch (submitError) {
      // The message is passed through whole, because on the one refusal that
      // matters it is the mitigation: a lost compare-and-swap wrote NOTHING, and
      // an operator who also transferred the money is told here — and only here
      // — to record the Payout directly (ADR 0026).
      toast.error(submitError instanceof Error ? submitError.message : "Failed to fulfil the request");
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
      setAmountError("Enter the amount that actually left the bank.");
      return;
    }
    if (!paidAt) {
      setAmountError(null);
      toast.error("Choose the date the money left the bank");
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
      toast.success("Request declined");
      await load();
    } catch (submitError) {
      toast.error(submitError instanceof Error ? submitError.message : "Failed to decline the request");
      setConfirmingDecline(false);
      await load();
    } finally {
      setSubmitting(false);
    }
  }

  function handleDecline(event: React.FormEvent) {
    event.preventDefault();
    const problem = declineReasonProblem(reason);
    setReasonError(problem);
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
      toast.success("Marked as processing — no payout recorded until the money lands");
      await load();
    } catch (submitError) {
      // Passed through whole, as the fulfilment refusal is. The one that matters
      // here names the colleague who already submitted a transfer, which is the
      // difference between "try again" and "do not send that money".
      toast.error(submitError instanceof Error ? submitError.message : "Failed to mark the request processing");
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
      toast.success("Marked as failed — the organization is told why");
      await load();
    } catch (submitError) {
      toast.error(submitError instanceof Error ? submitError.message : "Failed to mark the transfer failed");
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
    const problem = failureReasonProblem(failureReason);
    setFailureReasonError(problem);
    if (problem) {
      return;
    }
    setConfirmingFailure(true);
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading payout request...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>This account is not a platform operator.</AlertDescription>
      </Alert>
    );
  }

  if (error || !detail) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load payout request</AlertTitle>
        <AlertDescription>{error ?? "Payout request not found."}</AlertDescription>
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
  // different questions the moment `processing` exists: an in-flight request is
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
  const money = (cents: number) => formatPriceCents(cents, currency);
  const typedCents = parsePriceToCents(amount);
  const divergence = fulfilmentDivergence(typedCents, request.amount_cents, money);

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: "Operator", href: "/operator" },
          { label: "Payout requests", href: "/operator/payout-requests" },
          { label: organization.name },
        ]}
      />

      <PageHeader
        title={`${formatPriceCents(request.amount_cents, currency)} to ${organization.name}`}
        description={
          outstanding
            ? `Asked for by ${request.requested_by} · waiting ${waitingLabel(request.requested_at).toLowerCase()}`
            : `Asked for by ${request.requested_by} on ${new Date(request.requested_at).toLocaleDateString()}`
        }
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle>The ask</CardTitle>
            <CardDescription>
              What this organization asked for, and what it could have asked for at the time.
            </CardDescription>
          </div>
          <Badge variant={outstanding ? "default" : "secondary"} className="w-fit">
            {payoutRequestStatusLabel(request.status)}
          </Badge>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-6 sm:grid-cols-3">
            <div>
              <p className="text-sm text-muted-foreground">Requested</p>
              <p className="text-2xl font-semibold tabular-nums">
                {formatPriceCents(request.amount_cents, currency)}
              </p>
            </div>
            {/*
              The pair the whole screen is for. The snapshot cannot move; the
              live figure can and does, and the difference between them is the
              judgement the cap deliberately leaves to the operator (ADR 0026).
            */}
            <div>
              <p className="text-sm text-muted-foreground">Payable when asked</p>
              <p
                className={cn(
                  "text-2xl font-semibold tabular-nums",
                  snapshotPayable < 0 && "text-destructive",
                )}
              >
                {formatPriceCents(snapshotPayable, currency)}
              </p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Payable now</p>
              <p
                className={cn("text-2xl font-semibold tabular-nums", livePayable < 0 && "text-destructive")}
              >
                {formatPriceCents(livePayable, currency)}
              </p>
            </div>
          </div>

          <p className="text-sm text-muted-foreground">
            {request.amount_cents > livePayable
              ? `More than this organization can ask for today (${formatPriceCents(livePayable, currency)} has cleared). The request was within its balance when it was made and nothing re-checks it — paying it is your call.`
              : "Within what has cleared today. Withdrawable balance in full: " +
                formatPriceCents(detail.withdrawable_balance_cents, currency) +
                "."}
          </p>

          {request.note ? (
            <div>
              <p className="text-sm text-muted-foreground">Note from the organizer</p>
              <p>{request.note}</p>
            </div>
          ) : null}

          {/*
            A decline and a failure both carry a reason, in the same column, and
            the asker is shown it — but they are not the same news and must not
            be labelled as though they were. A decline is a judgement a person
            made; a failure is a bank sending the money back, with no judgement
            in it at all (ADR 0026 amendment).
          */}
          {request.resolution_reason ? (
            <p className="text-sm text-destructive">
              {request.status === "failed" ? "The bank rejected it" : "Declined"}:{" "}
              {request.resolution_reason}
            </p>
          ) : null}
          {request.resolved_by ? (
            <p className="text-sm text-muted-foreground">
              Answered by {request.resolved_by}
              {request.resolved_at ? ` on ${new Date(request.resolved_at).toLocaleDateString()}` : ""}.
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
                Transfer submitted by {request.transfer_submitted_by} on{" "}
                {new Date(request.transfer_submitted_at).toLocaleDateString()}
              </p>
              <p className="text-muted-foreground">
                {transferSentLabel(request.transfer_submitted_at)}
                {request.transfer_reference
                  ? ` · bank reference ${request.transfer_reference}`
                  : " · the bank gave no reference"}
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
                <p className="mt-2 font-medium text-destructive">
                  Nobody has confirmed this for over 72 hours. A transfer normally lands within 48.
                  Check it with the bank, then either record the payout or mark it failed.
                </p>
              ) : processing ? (
                <p className="mt-2 text-muted-foreground">
                  Waiting on the bank. A transfer can take up to 48 hours to land.
                </p>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Where to pay it</CardTitle>
          <CardDescription>
            The bank details as this request recorded them. A later change to the organization&apos;s payout
            profile does not touch them — this is the account the platform was told to pay.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <Detail label="Bank" value={profile.bank_name} />
          <Detail
            label="Account type"
            value={ACCOUNT_TYPE_LABELS[profile.account_type] ?? profile.account_type}
          />
          <Detail label="Account number" value={profile.account_number} mono />
          <Detail label="Account holder" value={profile.account_holder_name} />
          <Detail
            label={profile.tax_id_type === "ruc" ? "RUC" : "Cédula"}
            value={profile.tax_id_number}
            mono
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Organization</CardTitle>
          <CardDescription>Who is being paid, and everything else about their money.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Link
            href={`/operator/organizations/${organization.id}`}
            className="font-medium hover:underline"
          >
            {organization.name}
          </Link>
          <p className="text-sm text-muted-foreground">
            {organization.slug} · {currency} · withdrawable balance{" "}
            <span className={cn("tabular-nums", detail.withdrawable_balance_cents < 0 && "text-destructive")}>
              {formatPriceCents(detail.withdrawable_balance_cents, currency)}
            </span>
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
            <CardTitle>Answer this request</CardTitle>
            <CardDescription>
              {processing
                ? "The transfer is out there and the bank has not confirmed it. Record the payout when the money lands, or mark it failed when it comes back."
                : "Transfer the money to the account above first. If it settled, record it here and the request is marked paid in the same action. If it will take a day or two, mark it processing instead — that records the transfer without touching the ledger."}
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
                  <FormField id="fulfil-amount" label={`Amount transferred (${currency})`} error={amountError}>
                    <Input
                      value={amount}
                      onChange={(event) => setAmount(event.target.value)}
                      inputMode="decimal"
                      placeholder="0.00"
                      disabled={submitting}
                    />
                  </FormField>
                  <FormField id="fulfil-paid-at" label="Paid on">
                    <Input
                      type="date"
                      value={paidAt}
                      onChange={(event) => setPaidAt(event.target.value)}
                      disabled={submitting}
                    />
                  </FormField>
                  <FormField id="fulfil-note" label="Note (optional)">
                    <Input
                      value={note}
                      onChange={(event) => setNote(event.target.value)}
                      placeholder="Bank reference, batch, ..."
                      disabled={submitting}
                    />
                  </FormField>
                  <Button type="submit" disabled={submitting}>
                    {submitting ? "Recording..." : "Record payout"}
                  </Button>
                </div>
                {/*
                  Transferring less than was asked is an ordinary partial
                  fulfilment and needs no model of its own: what is recorded is
                  what moved, the request keeps what was asked, and the difference
                  is visible forever. Saying so here is cheaper than letting an
                  operator discover it afterwards.
                */}
                {divergence ? <p className="text-sm text-muted-foreground">{divergence}</p> : null}
              </form>
            ) : null}

            {/*
              MARKING THE TRANSFER SUBMITTED (#186). Offered only from `pending`:
              a request whose transfer is already in flight must not offer a
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
                    label="Or mark it processing — bank reference (optional)"
                    description="Use this when you have sent the transfer but the bank has not confirmed it. No payout is recorded yet, and the request stays in the queue."
                  >
                    <Input
                      value={transferReference}
                      onChange={(event) => setTransferReference(event.target.value)}
                      maxLength={TRANSFER_REFERENCE_MAX_LENGTH}
                      placeholder="PP-2026-0042, or leave blank"
                      disabled={submitting}
                    />
                  </FormField>
                  <Button type="submit" variant="outline" disabled={submitting}>
                    Mark as processing
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
                  label="Or the transfer bounced — what did the bank say?"
                  error={failureReasonError}
                  description="The organization is shown this, and it is what they act on. A wrong account number is the usual cause, and they fix it on their payout profile before asking again."
                >
                  <Textarea
                    value={failureReason}
                    onChange={(event) => setFailureReason(event.target.value)}
                    maxLength={DECLINE_REASON_MAX_LENGTH}
                    rows={3}
                    placeholder="Banco Pichincha returned it: the account number does not exist."
                    disabled={submitting}
                  />
                </FormField>
                <Button type="submit" variant="outline" disabled={submitting}>
                  Mark transfer failed
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
                  label="Or decline, with a reason"
                  error={reasonError}
                  description="The organization is shown this, and is free to ask again."
                >
                  <Textarea
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    maxLength={DECLINE_REASON_MAX_LENGTH}
                    rows={3}
                    placeholder="Your event is three months out — ask again once the doors have opened."
                    disabled={submitting}
                  />
                </FormField>
                <Button type="submit" variant="outline" disabled={submitting}>
                  Decline request
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
            <DialogTitle>More than this organization is owed</DialogTitle>
            <DialogDescription>
              {pendingAmountCents === null
                ? ""
                : `You are recording ${money(pendingAmountCents)} against a withdrawable balance of ${money(detail.withdrawable_balance_cents)}. That is accepted — the money has already left the bank — and the balance will go negative to say so.`}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingAmountCents(null)} disabled={submitting}>
              Go back
            </Button>
            <Button
              onClick={() => {
                if (pendingAmountCents !== null) {
                  void submitFulfilment(pendingAmountCents);
                }
              }}
              disabled={submitting}
            >
              {submitting ? "Recording..." : "Record it anyway"}
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
            <DialogTitle>You have already sent this transfer?</DialogTitle>
            <DialogDescription>
              {`Only press this if the money has left the bank. ${organization.name} will see that their transfer is on its way and will no longer be able to cancel the request. NO PAYOUT IS RECORDED yet — come back and record it when the money lands, or mark it failed if the bank sends it back.`}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            {transferReference.trim()
              ? `Bank reference: ${transferReference.trim()}`
              : "No bank reference — that is fine, PayPhone does not always give one."}
          </p>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmingProcessing(false)}
              disabled={submitting}
            >
              Go back
            </Button>
            <Button onClick={() => void submitProcessing()} disabled={submitting}>
              {submitting ? "Marking..." : "Yes, it is on its way"}
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
            <DialogTitle>The bank sent this transfer back?</DialogTitle>
            <DialogDescription>
              {`${organization.name} will see this on their payouts page as a failed transfer — not as a refusal — and will be pointed at their payout profile to correct it. This request cannot be reopened; they submit a new one. Nothing is undone in the ledger, because marking it processing recorded no payout.`}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm">{failureReason.trim()}</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmingFailure(false)} disabled={submitting}>
              Go back
            </Button>
            <Button variant="destructive" onClick={() => void submitFailure()} disabled={submitting}>
              {submitting ? "Marking..." : "Mark transfer failed"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* A decline is final and its reason reaches the organizer. */}
      <Dialog open={confirmingDecline} onOpenChange={(open) => !open && setConfirmingDecline(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Decline this request?</DialogTitle>
            <DialogDescription>
              {organization.name} will see this reason on their payouts page, and may submit a new
              request straight away. Declining is final — this request cannot be reopened.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm">{reason.trim()}</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmingDecline(false)} disabled={submitting}>
              Go back
            </Button>
            <Button variant="destructive" onClick={() => void submitDecline()} disabled={submitting}>
              {submitting ? "Declining..." : "Decline request"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
