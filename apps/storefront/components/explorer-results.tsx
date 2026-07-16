"use client";

import { useState } from "react";

import { Button } from "@ticket-pos/ui";

import { EXPLORER_PAGE_SIZE, type PublicEventCard, type PublicEventPage } from "@/lib/api";

import { EmptyState, EventGrid } from "./event-grid";

type ExplorerResultsProps = {
  initialEvents: PublicEventCard[];
  initialCursor: string | null;
  from?: string;
  to?: string;
  q?: string;
};

export function ExplorerResults({ initialEvents, initialCursor, from, to, q }: ExplorerResultsProps) {
  const [events, setEvents] = useState(initialEvents);
  const [cursor, setCursor] = useState(initialCursor);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function loadMore() {
    if (!cursor) return;
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams();
      if (q) params.set("q", q);
      if (from) params.set("from", from);
      if (to) params.set("to", to);
      params.set("cursor", cursor);
      params.set("limit", String(EXPLORER_PAGE_SIZE));

      const response = await fetch(`/api/events?${params.toString()}`);
      if (!response.ok) throw new Error("Could not load more events.");
      const page = (await response.json()) as PublicEventPage;
      setEvents((current) => [...current, ...page.events]);
      setCursor(page.next_cursor);
    } catch {
      setError("Could not load more events. Please try again.");
    } finally {
      setLoading(false);
    }
  }

  if (events.length === 0) {
    return (
      <EmptyState
        title="No events match your search"
        description="Try a different search or date range to find upcoming events."
      />
    );
  }

  return (
    <div className="space-y-6">
      <EventGrid events={events} />
      {error ? (
        <p role="alert" className="text-center text-sm text-destructive">
          {error}
        </p>
      ) : null}
      {cursor ? (
        <div className="flex justify-center">
          <Button variant="secondary" onClick={loadMore} disabled={loading} aria-busy={loading}>
            {loading ? "Loading…" : "Load more"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
