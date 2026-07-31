"use client";

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
  fetchOperatorOrganization,
  recordOperatorPayout,
} from "@/lib/operator-api";
import { exceedsWithdrawableBalance, formatPaidAtDate, todayISODate } from "@/lib/payouts";

type OperatorOrganizationClientProps = {
  organizationId: string;
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
  // Set only while the operator is being asked to confirm an over-balance
  // amount; carrying the parsed cents avoids re-parsing the field behind them.
  const [pendingAmountCents, setPendingAmountCents] = useState<number | null>(null);

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
      setPendingAmountCents(null);
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
    // Over the balance is allowed — the money has already moved — but it gets a
    // second look before it is written (ADR 0015).
    if (exceedsWithdrawableBalance(amountCents, detail.withdrawable_balance_cents)) {
      setPendingAmountCents(amountCents);
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
  } = detail;
  const currency = organization.currency;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[{ label: "Operator", href: "/operator" }, { label: organization.name }]}
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
        <CardContent>
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
            <DialogTitle>Amount exceeds the withdrawable balance</DialogTitle>
            <DialogDescription>
              You are recording {formatPriceCents(pendingAmountCents ?? 0, currency)} against a balance of{" "}
              {formatPriceCents(balanceCents, currency)}. This is accepted and will leave{" "}
              {organization.name} with a negative balance. Record it only if the money really moved.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPendingAmountCents(null)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => {
                if (pendingAmountCents !== null) {
                  void submitPayout(pendingAmountCents);
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
