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
  cn,
} from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import {
  type OperatorCurrencyTotals,
  type OperatorOrganizationRow,
  type OperatorPagination,
  fetchOperatorOrganizations,
  fetchOperatorSummary,
} from "@/lib/operator-api";

/**
 * Money that can legitimately be negative — an Organization that owes the
 * platform after a post-settlement reversal. Shown as-is, in the destructive
 * colour, never clamped: the sign is the information.
 */
function SignedAmount({ cents, currency }: { cents: number; currency: string }) {
  return (
    <span className={cn("tabular-nums", cents < 0 && "text-destructive")}>
      {formatPriceCents(cents, currency)}
    </span>
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
      <CardContent className="grid gap-6 sm:grid-cols-3">
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
      </CardContent>
    </Card>
  );
}

export function OperatorDashboardClient() {
  const [totals, setTotals] = useState<OperatorCurrencyTotals[]>([]);
  const [organizations, setOrganizations] = useState<OperatorOrganizationRow[]>([]);
  const [pagination, setPagination] = useState<OperatorPagination | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [summary, organizationsPage] = await Promise.all([
        fetchOperatorSummary(),
        fetchOperatorOrganizations(page),
      ]);
      setTotals(summary.totals);
      setOrganizations(organizationsPage.data);
      setPagination(organizationsPage.pagination);
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
  }, [page]);

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
        description="Platform revenue, every organization on the platform, and what each is owed."
      />

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

      <Card>
        <CardHeader>
          <CardTitle>Organizations</CardTitle>
          <CardDescription>Every Organization on the platform and its withdrawable balance.</CardDescription>
        </CardHeader>
        <CardContent>
          {organizations.length === 0 ? (
            <p className="text-sm text-muted-foreground">No organizations yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">Organization</th>
                    <th className="py-2 pr-4 font-medium">Slug</th>
                    <th className="py-2 pr-4 font-medium">Currency</th>
                    <th className="py-2 pr-4 font-medium">Events</th>
                    <th className="py-2 pr-4 font-medium">Withdrawable balance</th>
                  </tr>
                </thead>
                <tbody>
                  {organizations.map((organization) => (
                    <tr key={organization.id} className="border-b last:border-b-0">
                      <td className="py-3 pr-4">
                        <Link
                          href={`/operator/organizations/${organization.id}`}
                          className="font-medium hover:underline"
                        >
                          {organization.name}
                        </Link>
                      </td>
                      <td className="py-3 pr-4 font-mono text-xs text-muted-foreground">
                        {organization.slug}
                      </td>
                      <td className="py-3 pr-4">{organization.currency}</td>
                      <td className="py-3 pr-4 tabular-nums">{organization.events_count}</td>
                      <td className="py-3 pr-4">
                        <SignedAmount
                          cents={organization.withdrawable_balance_cents}
                          currency={organization.currency}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {pagination && pagination.total_pages > 1 ? (
            <div className="mt-4 flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                Page {pagination.page} of {pagination.total_pages} · {pagination.total} organizations
              </p>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pagination.page <= 1}
                  onClick={() => setPage((current) => Math.max(1, current - 1))}
                >
                  Previous
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pagination.page >= pagination.total_pages}
                  onClick={() => setPage((current) => current + 1)}
                >
                  Next
                </Button>
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
