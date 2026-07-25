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
  Label,
  Skeleton,
} from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import {
  channelSourceLabel,
  EMPTY_SALES_FILTERS,
  fetchSalesList,
  formatSaleTimestamp,
  hasActiveSalesFilters,
  paymentMethodLabel,
  rollupTicketTypes,
  salesListQuery,
  type SaleListRow,
  type SaleSortDir,
  type SaleSortField,
  type SalesFilters,
  type SalesListResponse,
} from "@/lib/sales-api";

import { useSalesRefreshSignal } from "./sales-refresh";

// TicketTypeOption is the minimal Ticket Type shape the ticket-type filter needs.
export type TicketTypeOption = {
  id: string;
  name: string;
};

type SalesListProps = {
  eventId: string;
  // The current page, read from the URL by the server and passed in as the
  // source of truth so page state lives in the URL.
  page: number;
  // The active filters, likewise read from the URL so the view is shareable and
  // survives a refresh.
  filters: SalesFilters;
  // The Event's Ticket Types, for the ticket-type filter dropdown.
  ticketTypes: TicketTypeOption[];
  // The current sort column and direction, also read from the URL so the sort is
  // shareable and survives a refresh.
  sort: SaleSortField;
  dir: SaleSortDir;
  // The Event timezone, used to render sold-at (null falls back to the viewer's).
  timezone: string | null;
};

// defaultDirFor is the direction a newly selected sort column starts in: newest
// or largest first for the date/amount columns, A→Z for the customer column.
function defaultDirFor(field: SaleSortField): SaleSortDir {
  return field === "customer" ? "asc" : "desc";
}

export function SalesList({ eventId, page, filters, ticketTypes, sort, dir, timezone }: SalesListProps) {
  const router = useRouter();
  const [result, setResult] = useState<SalesListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  // Bumped by the import section on a successful commit/undo; re-runs the fetch
  // below against the current view (same page/searchParams) so the list reflects
  // the change without losing the owner's filters/sort/page.
  const refreshSignal = useSalesRefreshSignal();

  // The URL query is the fetch key: refetch whenever the page, any filter, or the
  // sort changes (the server re-renders these props from the address bar).
  const filterKey = salesListQuery(page, filters, sort, dir);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchSalesList(eventId, page, filters, sort, dir)
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
    // filterKey encodes eventId-independent page+filter+sort state; eventId is
    // listed explicitly so a different Event also refetches, and refreshSignal so
    // a commit/undo re-fetches the current view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, filterKey, refreshSignal]);

  const goToPage = useCallback(
    (next: number) => {
      const target = Math.max(1, next);
      if (target === page) {
        return;
      }
      // Page state lives in the URL so the view survives a refresh and is shareable;
      // the current filters and sort are preserved as we page.
      router.push(`/events/${eventId}/sales${salesListQuery(target, filters, sort, dir)}`);
    },
    [eventId, page, filters, sort, dir, router],
  );

  const toggleSort = useCallback(
    (field: SaleSortField) => {
      // Clicking the active column flips its direction; a new column starts in its
      // natural default direction. Sorting resets to page 1 since the order changes,
      // but the active filters are preserved.
      const nextDir: SaleSortDir =
        field === sort ? (dir === "asc" ? "desc" : "asc") : defaultDirFor(field);
      router.push(`/events/${eventId}/sales${salesListQuery(1, filters, field, nextDir)}`);
    },
    [eventId, filters, sort, dir, router],
  );

  // applyFilters merges a filter change into the URL, resetting to page 1 (a new
  // filter starts a fresh result set) while preserving the current sort. Each
  // change is a history entry so the back button steps through filter changes.
  const applyFilters = useCallback(
    (patch: Partial<SalesFilters>) => {
      router.push(`/events/${eventId}/sales${salesListQuery(1, { ...filters, ...patch }, sort, dir)}`);
    },
    [eventId, filters, sort, dir, router],
  );

  const filtersActive = hasActiveSalesFilters(filters);

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
          Every individual Ticket Sale recorded for this Event. Click a column heading to sort.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <SalesFilterBar
          filters={filters}
          ticketTypes={ticketTypes}
          filtersActive={filtersActive}
          onApply={applyFilters}
        />
        {error ? (
          <p className="rounded-md border border-destructive/50 p-4 text-sm text-destructive">
            Couldn&apos;t load sales: {error}
          </p>
        ) : loading ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-12 w-full" />
            ))}
          </div>
        ) : !result || result.data.length === 0 ? (
          <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            {filtersActive
              ? "No sales match these filters."
              : "No sales recorded for this Event yet."}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="w-8 py-2 pr-2" />
                  <SortableHeader label="Customer" field="customer" sort={sort} dir={dir} onSort={toggleSort} />
                  <th className="py-2 pr-4 font-medium">Ticket types</th>
                  <SortableHeader label="Amount" field="amount" sort={sort} dir={dir} onSort={toggleSort} />
                  <SortableHeader label="Sold" field="sold_at" sort={sort} dir={dir} onSort={toggleSort} />
                  <SortableHeader label="Recorded" field="recorded_at" sort={sort} dir={dir} onSort={toggleSort} />
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

type SortableHeaderProps = {
  label: string;
  field: SaleSortField;
  sort: SaleSortField;
  dir: SaleSortDir;
  onSort: (field: SaleSortField) => void;
};

// SortableHeader is a column header that toggles the Sales list sort. The active
// column shows a direction arrow; clicking flips it, clicking another column
// switches to it. aria-sort exposes the state to assistive tech.
function SortableHeader({ label, field, sort, dir, onSort }: SortableHeaderProps) {
  const active = sort === field;
  return (
    <th
      className="py-2 pr-4 font-medium"
      aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(field)}
        className="inline-flex items-center gap-1 uppercase tracking-wide hover:text-foreground"
      >
        {label}
        <span aria-hidden className={active ? "text-foreground" : "text-muted-foreground/40"}>
          {active ? (dir === "asc" ? "▲" : "▼") : "↕"}
        </span>
      </button>
    </th>
  );
}

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

const CHANNEL_OPTIONS = [
  { value: "online", label: "Online" },
  { value: "in_person", label: "In person" },
  { value: "import", label: "Import" },
];

const SOURCE_OPTIONS = [
  { value: "direct", label: "Direct" },
  { value: "external_platform", label: "External platform" },
];

const PAYMENT_METHOD_OPTIONS = [
  { value: "cash", label: "Cash" },
  { value: "transfer", label: "Transfer" },
  { value: "payphone", label: "PayPhone" },
];

type SalesFilterBarProps = {
  filters: SalesFilters;
  ticketTypes: TicketTypeOption[];
  filtersActive: boolean;
  onApply: (patch: Partial<SalesFilters>) => void;
};

// SalesFilterBar renders the filter controls. Selects and dates apply on change
// (each a URL/history step); the search box applies on submit so typing does not
// flood the history. All changes flow up through onApply, which drives the URL.
function SalesFilterBar({ filters, ticketTypes, filtersActive, onApply }: SalesFilterBarProps) {
  const [search, setSearch] = useState(filters.q);

  // Keep the search box in sync when the URL changes underneath us (e.g. the
  // back button or a Clear).
  useEffect(() => {
    setSearch(filters.q);
  }, [filters.q]);

  return (
    <div className="space-y-3 rounded-md border bg-muted/20 p-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <form
          className="flex flex-col gap-1"
          onSubmit={(event) => {
            event.preventDefault();
            onApply({ q: search.trim() });
          }}
        >
          <Label htmlFor="sales-search">Search</Label>
          <div className="flex gap-2">
            <Input
              id="sales-search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Name, email, or reference"
              className="h-9"
            />
            <Button type="submit" variant="outline" size="sm">
              Search
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-status">Status</Label>
          <select
            id="sales-status"
            className={SELECT_CLASS}
            value={filters.status}
            onChange={(event) => onApply({ status: event.target.value })}
          >
            <option value="active">Active</option>
            <option value="reversed">Reversed</option>
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-ticket-type">Ticket type</Label>
          <select
            id="sales-ticket-type"
            className={SELECT_CLASS}
            value={filters.ticketTypeId}
            onChange={(event) => onApply({ ticketTypeId: event.target.value })}
          >
            <option value="">All ticket types</option>
            {ticketTypes.map((type) => (
              <option key={type.id} value={type.id}>
                {type.name}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-channel">Channel</Label>
          <select
            id="sales-channel"
            className={SELECT_CLASS}
            value={filters.channel}
            onChange={(event) => onApply({ channel: event.target.value })}
          >
            <option value="">All channels</option>
            {CHANNEL_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-source">Source</Label>
          <select
            id="sales-source"
            className={SELECT_CLASS}
            value={filters.source}
            onChange={(event) => onApply({ source: event.target.value })}
          >
            <option value="">All sources</option>
            {SOURCE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-payment-method">Payment method</Label>
          <select
            id="sales-payment-method"
            className={SELECT_CLASS}
            value={filters.paymentMethod}
            onChange={(event) => onApply({ paymentMethod: event.target.value })}
          >
            <option value="">All payment methods</option>
            {PAYMENT_METHOD_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-sold-from">Sold from</Label>
          <Input
            id="sales-sold-from"
            type="date"
            value={filters.soldFrom}
            max={filters.soldTo || undefined}
            onChange={(event) => onApply({ soldFrom: event.target.value })}
            className="h-9"
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-sold-to">Sold to</Label>
          <Input
            id="sales-sold-to"
            type="date"
            value={filters.soldTo}
            min={filters.soldFrom || undefined}
            onChange={(event) => onApply({ soldTo: event.target.value })}
            className="h-9"
          />
        </div>
      </div>

      {filtersActive ? (
        <div className="flex justify-end">
          <Button type="button" variant="ghost" size="sm" onClick={() => onApply(EMPTY_SALES_FILTERS)}>
            Clear filters
          </Button>
        </div>
      ) : null}
    </div>
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
        <td className="py-3 pr-4 text-muted-foreground">{formatSaleTimestamp(sale.recorded_at, timezone)}</td>
        <td className="py-3 pr-4">{channelSourceLabel(sale.channel, sale.source)}</td>
        <td className="py-3 pr-4 font-mono text-xs">{sale.confirmation_ref}</td>
      </tr>
      {expanded ? (
        <tr className="bg-muted/30">
          <td />
          <td className="py-3 pr-4 text-muted-foreground" colSpan={7}>
            <div className="flex flex-wrap gap-x-8 gap-y-1">
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
