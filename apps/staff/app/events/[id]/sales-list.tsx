"use client";

import { useCallback, useEffect, useState } from "react";

import { useRouter } from "next/navigation";

import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Skeleton,
} from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import {
  channelSourceLabel,
  fetchSalesList,
  formatSaleTimestamp,
  paymentMethodLabel,
  rollupTicketTypes,
  type SaleListRow,
  type SalesListResponse,
} from "@/lib/sales-api";

import { useSalesRefreshSignal } from "./sales-refresh";

type SalesListProps = {
  eventId: string;
  // The current page, read from the URL by the server and passed in as the
  // source of truth so page state lives in the URL.
  page: number;
  // The Event timezone, used to render sold-at (null falls back to the viewer's).
  timezone: string | null;
};

export function SalesList({ eventId, page, timezone }: SalesListProps) {
  const router = useRouter();
  const [result, setResult] = useState<SalesListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  // Bumped by the import section on a successful commit/undo; re-runs the fetch
  // below against the current view (same page/searchParams) so the list reflects
  // the change without losing the owner's filters/sort/page.
  const refreshSignal = useSalesRefreshSignal();

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchSalesList(eventId, page)
      .then((data) => {
        if (!cancelled) {
          setResult(data);
        }
      })
      .catch((fetchError: unknown) => {
        if (!cancelled) {
          setError(fetchError instanceof Error ? fetchError.message : "Failed to load sales");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [eventId, page, refreshSignal]);

  const goToPage = useCallback(
    (next: number) => {
      const target = Math.max(1, next);
      if (target === page) {
        return;
      }
      // Page state lives in the URL so the view survives a refresh and is shareable.
      const query = target === 1 ? "" : `?page=${target}`;
      router.push(`/events/${eventId}/sales${query}`);
    },
    [eventId, page, router],
  );

  function toggleRow(id: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }

  const pagination = result?.pagination;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sales</CardTitle>
        <CardDescription>
          Every individual Ticket Sale recorded for this Event, most recent first.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {error ? (
          <p className="rounded-md border border-destructive/50 p-4 text-sm text-destructive">{error}</p>
        ) : loading ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-12 w-full" />
            ))}
          </div>
        ) : !result || result.data.length === 0 ? (
          <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            No sales recorded for this Event yet.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="w-8 py-2 pr-2" />
                  <th className="py-2 pr-4 font-medium">Customer</th>
                  <th className="py-2 pr-4 font-medium">Ticket types</th>
                  <th className="py-2 pr-4 font-medium">Amount</th>
                  <th className="py-2 pr-4 font-medium">Sold</th>
                  <th className="py-2 pr-4 font-medium">Channel</th>
                  <th className="py-2 pr-4 font-medium">Reference</th>
                </tr>
              </thead>
              {result.data.map((sale) => (
                <SaleRows
                  key={sale.id}
                  sale={sale}
                  timezone={timezone}
                  expanded={expanded.has(sale.id)}
                  onToggle={() => toggleRow(sale.id)}
                />
              ))}
            </table>
          </div>
        )}

        {pagination && pagination.total > 0 ? (
          <PaginationControls
            page={pagination.page}
            totalPages={pagination.total_pages}
            total={pagination.total}
            onGo={goToPage}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

type SaleRowsProps = {
  sale: SaleListRow;
  timezone: string | null;
  expanded: boolean;
  onToggle: () => void;
};

function SaleRows({ sale, timezone, expanded, onToggle }: SaleRowsProps) {
  const name = `${sale.customer_first_name} ${sale.customer_last_name}`.trim() || "—";
  const reversed = sale.status !== "active";

  return (
    <tbody className="border-b last:border-b-0">
      <tr
        className="cursor-pointer align-top hover:bg-muted/50"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <td className="py-3 pr-2 text-muted-foreground">{expanded ? "▾" : "▸"}</td>
        <td className="py-3 pr-4">
          <div className="font-medium">{name}</div>
          <div className="text-muted-foreground">{sale.customer_email}</div>
          {reversed ? (
            <Badge variant="destructive" className="mt-1">
              Reversed
            </Badge>
          ) : null}
        </td>
        <td className="py-3 pr-4">{rollupTicketTypes(sale.ticket_types)}</td>
        <td className="py-3 pr-4 tabular-nums">{formatPriceCents(sale.amount_cents, sale.currency)}</td>
        <td className="py-3 pr-4">{formatSaleTimestamp(sale.sold_at, timezone)}</td>
        <td className="py-3 pr-4">{channelSourceLabel(sale.channel, sale.source)}</td>
        <td className="py-3 pr-4 font-mono text-xs">{sale.confirmation_ref}</td>
      </tr>
      {expanded ? (
        <tr className="bg-muted/30">
          <td />
          <td className="py-3 pr-4 text-muted-foreground" colSpan={6}>
            <div className="flex flex-wrap gap-x-8 gap-y-1">
              <span>
                <span className="font-medium text-foreground">Recorded:</span>{" "}
                {formatSaleTimestamp(sale.recorded_at, timezone)}
              </span>
              <span>
                <span className="font-medium text-foreground">Payment method:</span>{" "}
                {paymentMethodLabel(sale.payment_method)}
              </span>
            </div>
          </td>
        </tr>
      ) : null}
    </tbody>
  );
}

type PaginationControlsProps = {
  page: number;
  totalPages: number;
  total: number;
  onGo: (page: number) => void;
};

function PaginationControls({ page, totalPages, total, onGo }: PaginationControlsProps) {
  const [jumpValue, setJumpValue] = useState("");

  function handleJump() {
    const target = Number.parseInt(jumpValue, 10);
    if (Number.isFinite(target)) {
      onGo(Math.min(Math.max(1, target), totalPages));
    }
    setJumpValue("");
  }

  return (
    <div className="flex flex-col gap-3 border-t pt-4 text-sm sm:flex-row sm:items-center sm:justify-between">
      <span className="text-muted-foreground">
        {total} sale{total === 1 ? "" : "s"} · page {page} of {totalPages}
      </span>
      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => onGo(page - 1)}>
          Previous
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onGo(page + 1)}
        >
          Next
        </Button>
        {totalPages > 1 ? (
          <form
            className="flex items-center gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              handleJump();
            }}
          >
            <Input
              type="number"
              min={1}
              max={totalPages}
              value={jumpValue}
              onChange={(event) => setJumpValue(event.target.value)}
              placeholder="Page"
              className="h-8 w-20"
              aria-label="Jump to page"
            />
            <Button type="submit" variant="outline" size="sm">
              Go
            </Button>
          </form>
        ) : null}
      </div>
    </div>
  );
}
