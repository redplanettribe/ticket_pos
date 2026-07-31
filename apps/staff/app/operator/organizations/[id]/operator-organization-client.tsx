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
  cn,
  toast,
} from "@ticket-pos/ui";

import {
  formatEventStartDate,
  formatPriceCents,
  parsePriceToCents,
  statusBadgeVariant,
} from "@/lib/events-api";
import {
  type OperatorOrganizationDetail,
  type OperatorPayoutRequestRow,
  fetchOperatorOrganization,
  recordOperatorPayout,
} from "@/lib/operator-api";
import { isOutstanding, payoutRequestStatusLabel } from "@/lib/payout-requests";
import {
  exceedsWithdrawableBalance,
  formatPaidAtDate,
  outstandingPayoutRequest,
  todayISODate,
} from "@/lib/payouts";

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
      const message = loadError instanceof Error ? loadError.message : "Failed to load organization";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
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
      toast.success("Payout recorded");
      // Re-read rather than patch locally: the Withdrawable Balance is the
      // server's arithmetic, and this page shows it in two places.
      await load();
    } catch (submitError) {
      toast.error(submitError instanceof Error ? submitError.message : "Failed to record payout");
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
      setAmountError("Enter an amount greater than zero.");
      return;
    }
    if (!paidAt) {
      setAmountError(null);
      toast.error("Choose the date the money left the bank");
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
    return <p className="text-sm text-muted-foreground">Loading organization...</p>;
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
        <AlertTitle>Could not load organization</AlertTitle>
        <AlertDescription>{error ?? "Organization not found."}</AlertDescription>
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
  // The ask this Organization is waiting on, if any. It survives a direct
  // Payout — nothing here auto-closes a request (ADR 0026) — so an operator who
  // records one settlement and comes back for another is warned both times.
  const outstanding = outstandingPayoutRequest(payoutRequests);

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[{ label: "Organizations", href: "/operator/organizations" }, { label: organization.name }]}
      />

      <PageHeader
        title={organization.name}
        description={`${organization.slug} · ${currency} · created ${new Date(organization.created_at).toLocaleDateString()}`}
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
          <CardTitle>Balances</CardTitle>
          <CardDescription>
            Net proceeds from this Organization&apos;s online sales, less what has already been paid out — in full, and
            the part of it that has cleared.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6 sm:grid-cols-2">
          <div>
            <p className="text-sm text-muted-foreground">Withdrawable balance</p>
            <p
              className={cn(
                "text-3xl font-semibold tabular-nums",
                balanceCents < 0 && "text-destructive",
              )}
            >
              {formatPriceCents(balanceCents, currency)}
            </p>
            {balanceCents < 0 ? (
              <p className="mt-2 text-sm text-muted-foreground">
                Negative: this Organization owes the platform after a post-settlement reversal.
              </p>
            ) : null}
          </div>
          <div>
            <p className="text-sm text-muted-foreground">Payable balance</p>
            <p
              className={cn(
                "text-3xl font-semibold tabular-nums",
                payableCents < 0 && "text-destructive",
              )}
            >
              {formatPriceCents(payableCents, currency)}
            </p>
            <p className="mt-2 text-sm text-muted-foreground">
              {payableCents < balanceCents
                ? "What this Organization may ask for today. The rest is sales recorded today, or with a reversal still open, which have not cleared."
                : "Every sale behind this balance has cleared."}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Record payout</CardTitle>
          <CardDescription>
            Record money that has already left the bank. Current balance:{" "}
            <span className={cn("tabular-nums", balanceCents < 0 && "text-destructive")}>
              {formatPriceCents(balanceCents, currency)}
            </span>
            .
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
              <AlertTitle>This organization is already waiting on a payout request</AlertTitle>
              <AlertDescription className="space-y-2">
                <p>
                  {organization.name} asked for{" "}
                  <span className="font-medium tabular-nums">
                    {formatPriceCents(outstanding.amount_cents, currency)}
                  </span>{" "}
                  on {new Date(outstanding.requested_at).toLocaleDateString()}. Fulfilling the
                  request records the payout and answers the ask in one step. Recording here leaves
                  the request open, and risks paying twice.
                </p>
                <Button asChild variant="outline" size="sm">
                  <Link href={`/operator/payout-requests/${outstanding.id}`}>
                    Fulfil the request instead
                  </Link>
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          <form className="grid gap-4 sm:grid-cols-[1fr_1fr_2fr_auto] sm:items-end" onSubmit={handleSubmit}>
            <FormField id="payout-amount" label={`Amount (${currency})`} error={amountError}>
              <Input
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
                inputMode="decimal"
                placeholder="0.00"
                disabled={submitting}
              />
            </FormField>
            <FormField id="payout-paid-at" label="Paid on">
              <Input
                type="date"
                value={paidAt}
                onChange={(event) => setPaidAt(event.target.value)}
                disabled={submitting}
              />
            </FormField>
            <FormField id="payout-note" label="Note (optional)">
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
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Events</CardTitle>
          <CardDescription>Every Event this Organization runs, newest first.</CardDescription>
        </CardHeader>
        <CardContent>
          {events.length === 0 ? (
            <p className="text-sm text-muted-foreground">No events yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">Event</th>
                    <th className="py-2 pr-4 font-medium">Status</th>
                    <th className="py-2 pr-4 font-medium">Starts</th>
                    <th className="py-2 pr-4 font-medium">Discoverable</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((event) => (
                    <tr key={event.id} className="border-b last:border-b-0">
                      <td className="py-3 pr-4 font-medium">{event.name}</td>
                      <td className="py-3 pr-4">
                        <Badge variant={statusBadgeVariant(event.status)} className="w-fit capitalize">
                          {event.status}
                        </Badge>
                      </td>
                      <td className="py-3 pr-4">{formatEventStartDate(event.starts_at, null)}</td>
                      <td className="py-3 pr-4 text-muted-foreground">
                        {event.discoverable ? "Yes" : "No"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Payout history</CardTitle>
          <CardDescription>Every Payout recorded against this Organization, newest first.</CardDescription>
        </CardHeader>
        <CardContent>
          {payouts.length === 0 ? (
            <p className="text-sm text-muted-foreground">No payouts recorded yet.</p>
          ) : (
            <div className="space-y-3">
              {payouts.map((payout) => (
                <div
                  key={payout.id}
                  className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div>
                    <p className="font-medium tabular-nums">
                      {formatPriceCents(payout.amount_cents, currency)}
                    </p>
                    {payout.note ? (
                      <p className="text-sm text-muted-foreground">{payout.note}</p>
                    ) : null}
                  </div>
                  <div className="sm:text-right">
                    <p className="text-sm text-muted-foreground">{formatPaidAtDate(payout.paid_at)}</p>
                    <p className="text-xs text-muted-foreground">
                      {payout.recorded_by ? `Recorded by ${payout.recorded_by}` : "Recorded directly in the database"}
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
          <CardTitle>Payout requests</CardTitle>
          <CardDescription>
            Every payout this Organization has asked for, newest first.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {payoutRequests.length === 0 ? (
            <p className="text-sm text-muted-foreground">This organization has never asked to be paid.</p>
          ) : (
            <div className="space-y-3">
              {payoutRequests.map((request) => (
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
                          {formatPriceCents(request.amount_cents, currency)}
                        </Link>
                      ) : (
                        formatPriceCents(request.amount_cents, currency)
                      )}
                    </p>
                    {request.note ? (
                      <p className="text-sm text-muted-foreground">{request.note}</p>
                    ) : null}
                    {request.resolution_reason ? (
                      <p className="text-sm text-destructive">Declined: {request.resolution_reason}</p>
                    ) : null}
                    <p className="text-sm text-muted-foreground">
                      Paying to {request.payout_profile.bank_name}{" "}
                      {request.payout_profile.account_number_masked}
                    </p>
                  </div>
                  <div className="sm:text-right">
                    <p className="text-sm font-medium">{payoutRequestStatusLabel(request.status)}</p>
                    <p className="text-sm text-muted-foreground">
                      {new Date(request.requested_at).toLocaleDateString()} · {request.requested_by}
                    </p>
                  </div>
                </div>
              ))}
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
                ? "This organization is waiting on a payout request"
                : "Amount exceeds the withdrawable balance"}
            </DialogTitle>
            <DialogDescription asChild>
              <div className="space-y-3">
                {pending?.outstandingRequest ? (
                  <p>
                    {organization.name} has an outstanding request for{" "}
                    <span className="font-medium tabular-nums">
                      {formatPriceCents(pending.outstandingRequest.amount_cents, currency)}
                    </span>
                    , asked for on{" "}
                    {new Date(pending.outstandingRequest.requested_at).toLocaleDateString()} by{" "}
                    {pending.outstandingRequest.requested_by}. Recording{" "}
                    {formatPriceCents(pending.amountCents, currency)} here does not answer it — the
                    request stays open, and another operator may still fulfil it. Only record
                    directly if this is a different transfer.
                  </p>
                ) : null}
                {pending?.exceedsBalance ? (
                  <p>
                    You are recording {formatPriceCents(pending.amountCents, currency)} against a
                    balance of {formatPriceCents(balanceCents, currency)}. This is accepted and will
                    leave {organization.name} with a negative balance. Record it only if the money
                    really moved.
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
              Cancel
            </Button>
            {pending?.outstandingRequest ? (
              <Button asChild variant="secondary">
                <Link href={`/operator/payout-requests/${pending.outstandingRequest.id}`}>
                  Fulfil the request instead
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
              {submitting ? "Recording..." : "Record anyway"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
