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
} from "@/lib/operator-api";
import {
  DECLINE_REASON_MAX_LENGTH,
  declineReasonProblem,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  payoutRequestStatusLabel,
  waitingLabel,
} from "@/lib/payout-requests";
import { exceedsWithdrawableBalance, todayISODate } from "@/lib/payouts";

// One payout request, with everything needed to execute the transfer (#176) and
// the two ways to answer it (#177, ADR 0026).
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
// hand, as they always have, and then records it here — and that one action also
// ends the ask, so there is no second step to forget.
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
  const outstanding = request.status === "pending";
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

          {/* A decline always carries its reason, and the asker is shown it. */}
          {request.decline_reason ? (
            <p className="text-sm text-destructive">Declined: {request.decline_reason}</p>
          ) : null}
          {request.resolved_by ? (
            <p className="text-sm text-muted-foreground">
              Answered by {request.resolved_by}
              {request.resolved_at ? ` on ${new Date(request.resolved_at).toLocaleDateString()}` : ""}.
            </p>
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
        ANSWERING THE ASK (#177, ADR 0026). Shown only while the request is
        outstanding: all three end states are final, and a form offering to
        answer an answered request would be offering something the API refuses.

        Two answers, side by side, because they are the only two there are.
        There is no "approve" — the operator transfers the money by hand first
        and records it second, which is what they always did, and an
        approved-but-unpaid request would be a debt with a state name.
      */}
      {outstanding ? (
        <Card>
          <CardHeader>
            <CardTitle>Answer this request</CardTitle>
            <CardDescription>
              Transfer the money to the account above first. Recording it here marks the request paid
              in the same action — there is no second step.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-8">
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
