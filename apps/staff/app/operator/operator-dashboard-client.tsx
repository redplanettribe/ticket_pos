"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

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

import { formatPriceCents } from "@/lib/events-api";
import {
  OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE,
  type OperatorCurrencyTotals,
  fetchOperatorOrganizations,
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
function KeptFeeNote({ totals }: { totals: OperatorCurrencyTotals }) {
  const keptCents = totals.kept_fee_cents + totals.kept_fee_iva_cents;
  if (keptCents === 0) {
    return null;
  }
  return (
    <p className="text-sm text-muted-foreground">
      Includes {formatPriceCents(keptCents, totals.currency)} in fees and fee IVA kept on reversed
      sales.
    </p>
  );
}

function TotalsCard({ totals }: { totals: OperatorCurrencyTotals }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{totals.currency}</CardTitle>
        <CardDescription>
          Accumulated platform revenue and what the platform currently owes, in {totals.currency}.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-6 sm:grid-cols-3">
          <div>
            <p className="text-sm text-muted-foreground">Platform fees</p>
            <p className="text-2xl font-semibold tabular-nums">
              {formatPriceCents(totals.platform_fee_cents, totals.currency)}
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">Fee IVA</p>
            <p className="text-2xl font-semibold tabular-nums">
              {formatPriceCents(totals.fee_iva_cents, totals.currency)}
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">Total owed</p>
            <p className="text-2xl font-semibold tabular-nums">
              {formatPriceCents(totals.total_owed_cents, totals.currency)}
            </p>
          </div>
        </div>
        <KeptFeeNote totals={totals} />
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
  count: number;
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
  const [totals, setTotals] = useState<OperatorCurrencyTotals[]>([]);
  const [organizationCount, setOrganizationCount] = useState(0);
  const [pendingPayoutRequests, setPendingPayoutRequests] = useState(0);
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
      const [summary, organizationsPage, payoutRequestCount] = await Promise.all([
        fetchOperatorSummary(),
        fetchOperatorOrganizations(1, OPERATOR_ORGANIZATIONS_COUNT_PAGE_SIZE),
        fetchOperatorPendingPayoutRequestCount(),
      ]);
      setTotals(summary.totals);
      setOrganizationCount(organizationsPage.pagination.total);
      setPendingPayoutRequests(payoutRequestCount.pending_count);
      setForbidden(false);
    } catch (loadError) {
      const message = loadError instanceof Error ? loadError.message : "Failed to load";
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
    return <p className="text-sm text-muted-foreground">Loading operator dashboard...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>This account is not a platform operator.</AlertDescription>
      </Alert>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the operator dashboard</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Operator"
        description="Platform revenue, and what is waiting to be done across every organization."
      />

      <div className="grid gap-6 sm:grid-cols-2">
        {/*
          Payout requests first: it is the one figure here that somebody is
          WAITING on, while the organization count is a standing fact.
        */}
        <CountCard
          title="Payout requests"
          description="Every organization waiting to be paid, across the platform, longest wait first."
          count={pendingPayoutRequests}
          href="/operator/payout-requests"
          linkLabel="Open the queue"
        />
        <CountCard
          title="Organizations"
          description="Every organization on the platform and its withdrawable balance."
          count={organizationCount}
          href="/operator/organizations"
          linkLabel="View organizations"
        />
      </div>

      {totals.length === 0 ? (
        <Card>
          <CardContent className="py-10">
            <p className="text-muted-foreground">No platform revenue recorded yet.</p>
          </CardContent>
        </Card>
      ) : (
        // One card per currency: there is no exchange rate anywhere on this
        // platform, so these numbers are never added together.
        totals.map((currencyTotals) => (
          <TotalsCard key={currencyTotals.currency} totals={currencyTotals} />
        ))
      )}
    </div>
  );
}
