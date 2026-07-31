"use client";

import { useCallback, useEffect, useState } from "react";

import { Alert, AlertDescription, AlertTitle, Card, CardContent } from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import { payableBalanceExplanation } from "@/lib/payable-balance";
import { formatPaidAtDate } from "@/lib/payouts";

import { PayoutRequestsSection } from "./payout-requests-section";

// The whole money-out story on one page (#190): what the platform owes, where to
// pay it, the ask, and what came back. It used to be one section on a settings
// page that also held the logo and the danger zone; nothing about it changed in
// the move except the labels, which now say what the glossary says.

type Payout = {
  id: string;
  amount_cents: number;
  /** A calendar day ("YYYY-MM-DD"), not an instant. */
  paid_at: string;
  note: string | null;
};

type PayoutsSummary = {
  /** What the platform owes. Signed: negative after a post-settlement reversal. */
  withdrawable_balance_cents: number;
  /**
   * The part of it that has cleared and may be asked for today (ADR 0026).
   * Signed too, never larger than the figure above, and negative when a
   * settlement got ahead of what had cleared.
   */
  payable_balance_cents: number;
  currency: string;
  payouts: Payout[];
};

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, { headers: { "Content-Type": "application/json" } });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new Error(envelope.error?.message ?? "Request failed");
  }
  if (envelope.data === null) {
    throw new Error("Empty response");
  }
  return envelope.data;
}

export function PayoutsPageClient() {
  const [payouts, setPayouts] = useState<PayoutsSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setPayouts(await fetchJSON<PayoutsSummary>("/api/settings/organization/payouts"));
      setForbidden(false);
    } catch (loadError) {
      const message = loadError instanceof Error ? loadError.message : "Failed to load payouts";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading payouts...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>You need Org Admin access to manage payouts.</AlertDescription>
      </Alert>
    );
  }

  if (error || !payouts) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load payouts</AlertTitle>
        <AlertDescription>{error ?? "Unknown error"}</AlertDescription>
      </Alert>
    );
  }

  return (
    <Card>
      <CardContent className="space-y-6 pt-6">
        {/*
          Both balances, side by side, with the gap between them in words.
          Showing only the smaller figure would be simpler and would send
          every organizer who sold this morning to support asking why they
          are being offered less than they earned (ADR 0026).

          The labels are the glossary's own — an organizer phoning a Platform
          Operator about their payable balance is then speaking the words the
          operator's screens use, and "available balance" is a term CONTEXT.md
          says to avoid.
        */}
        <div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <p className="text-sm text-muted-foreground">Withdrawable balance</p>
              <p className="text-3xl font-semibold tabular-nums">
                {formatPriceCents(payouts.withdrawable_balance_cents, payouts.currency)}
              </p>
              <p className="text-xs text-muted-foreground">Everything your online sales have earned you so far.</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Payable balance</p>
              <p className="text-3xl font-semibold tabular-nums">
                {formatPriceCents(payouts.payable_balance_cents, payouts.currency)}
              </p>
              <p className="text-xs text-muted-foreground">
                Sales clear overnight, so today&apos;s are not here yet.
              </p>
            </div>
          </div>
          <p className="mt-4 text-sm text-muted-foreground">
            {payableBalanceExplanation(
              payouts.withdrawable_balance_cents,
              payouts.payable_balance_cents,
              (cents) => formatPriceCents(cents, payouts.currency),
            )}
          </p>
        </div>

        {/*
          Where the money goes, the ask to be paid, and what became of every
          earlier ask (#175, ADR 0026). It sits between the balances and the
          payout history because that is the order the questions arrive in:
          how much do I have, can I have it, and what have I been paid
          before. It reads the Payable Balance from the summary above rather
          than fetching its own — two readings of the same money on one page
          would eventually disagree.
        */}
        <PayoutRequestsSection
          currency={payouts.currency}
          payableBalanceCents={payouts.payable_balance_cents}
        />

        <div className="space-y-3">
          <p className="text-sm font-medium">Payout history</p>
          {payouts.payouts.length === 0 ? (
            <p className="text-sm text-muted-foreground">No payouts recorded yet.</p>
          ) : (
            <div className="space-y-3">
              {payouts.payouts.map((payout) => (
                <div
                  key={payout.id}
                  className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div>
                    <p className="font-medium tabular-nums">
                      {formatPriceCents(payout.amount_cents, payouts.currency)}
                    </p>
                    {payout.note ? <p className="text-sm text-muted-foreground">{payout.note}</p> : null}
                  </div>
                  <p className="text-sm text-muted-foreground">{formatPaidAtDate(payout.paid_at)}</p>
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
