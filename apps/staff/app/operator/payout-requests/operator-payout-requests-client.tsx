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
  PageHeader,
} from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import {
  type OperatorPagination,
  type OperatorPayoutRequestQueueItem,
  fetchOperatorPayoutRequests,
} from "@/lib/operator-api";
import { payoutRequestStatusLabel, transferSentLabel, waitingLabel } from "@/lib/payout-requests";

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

function QueueRow({ item }: { item: OperatorPayoutRequestQueueItem }) {
  const { request, organization } = item;
  const askedForAll = request.amount_cents >= request.payable_balance_cents;
  const processing = request.status === "processing";

  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/payout-requests/${request.id}`} className="font-medium hover:underline">
          {organization.name}
        </Link>
        <p className="font-mono text-xs text-muted-foreground">{organization.slug}</p>
      </td>
      <td className="py-3 pr-4">
        <p className="font-medium tabular-nums">
          {formatPriceCents(request.amount_cents, organization.currency)}
        </p>
        {/*
          The ask beside what could have been asked for when it was made. The
          two together are what tell an organization that asked for everything
          it had from one that asked for a slice — and the live figure, which
          may have moved since, is on the request itself.
        */}
        <p className="text-xs text-muted-foreground">
          {askedForAll ? "all of " : "of "}
          {formatPriceCents(request.payable_balance_cents, organization.currency)} payable then
        </p>
      </td>
      <td className="py-3 pr-4">
        <p className="tabular-nums">{waitingLabel(request.requested_at)}</p>
        <p className="text-xs text-muted-foreground">
          {new Date(request.requested_at).toLocaleDateString()}
        </p>
      </td>
      {/*
        WHAT KIND OF WORK THIS ROW IS. "Waiting" means nobody has touched it;
        "Processing" means a colleague already sent the money and is waiting on
        the bank — which is a different next action, and confusing the two is how
        the same request gets transferred twice.

        The stale flag rides beside it rather than replacing it: a stale request
        is still processing, and what changed is only that nobody has confirmed
        it for three days. It is destructive-weight because it is the ONLY
        backstop there is — no reconciler, no timeout, nothing else will notice.
      */}
      <td className="py-3 pr-4">
        <Badge variant={processing ? "secondary" : "default"} className="w-fit">
          {payoutRequestStatusLabel(request.status)}
        </Badge>
        {request.transfer_stale ? (
          <p className="mt-1 text-xs font-medium text-destructive">
            Unconfirmed for over 72 hours — check the transfer.
          </p>
        ) : request.transfer_submitted_at ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {transferSentLabel(request.transfer_submitted_at)} by {request.transfer_submitted_by}
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
    return <p className="text-sm text-muted-foreground">Loading payout requests...</p>;
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
        <AlertTitle>Could not load payout requests</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <Breadcrumb items={[{ label: "Operator", href: "/operator" }, { label: "Payout requests" }]} />

      <PageHeader
        title="Payout requests"
        description="Every organization waiting to be paid, longest wait first. A transfer nobody has confirmed in three days is flagged."
      />

      <Card>
        <CardHeader>
          <CardTitle>Waiting for an answer</CardTitle>
          <CardDescription>
            Every outstanding request across every organization — the ones nobody has answered and the
            ones whose transfer is already in flight. Account numbers are shown in part here; open a
            request to see the full bank details and pay it.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nobody is waiting to be paid.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">Organization</th>
                    <th className="py-2 pr-4 font-medium">Requested</th>
                    <th className="py-2 pr-4 font-medium">Waiting</th>
                    <th className="py-2 pr-4 font-medium">Status</th>
                    <th className="py-2 pr-4 font-medium">Paying to</th>
                    <th className="py-2 pr-4 font-medium">Asked by</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <QueueRow key={item.request.id} item={item} />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {pagination && pagination.total_pages > 1 ? (
            <div className="mt-4 flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                Page {pagination.page} of {pagination.total_pages} · {pagination.total} waiting
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
