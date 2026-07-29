"use client";

import { useTranslations } from "next-intl";
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
  tags?: string[];
};

export function ExplorerResults({
  initialEvents,
  initialCursor,
  from,
  to,
  q,
  tags,
}: ExplorerResultsProps) {
  const t = useTranslations("explorer");
  const [events, setEvents] = useState(initialEvents);
  const [cursor, setCursor] = useState(initialCursor);
  const [loading, setLoading] = useState(false);
  // Whether the last "Load more" failed, not the sentence saying so: the words
  // are the catalog's and are looked up at render, so state holds no copy.
  const [failed, setFailed] = useState(false);

  async function loadMore() {
    if (!cursor) return;
    setLoading(true);
    setFailed(false);
    try {
      const params = new URLSearchParams();
      if (q) params.set("q", q);
      if (from) params.set("from", from);
      if (to) params.set("to", to);
      if (tags && tags.length > 0) params.set("tags", tags.join(","));
      params.set("cursor", cursor);
      params.set("limit", String(EXPLORER_PAGE_SIZE));

      const response = await fetch(`/api/events?${params.toString()}`);
      if (!response.ok) throw new Error(`events request failed: ${response.status}`);
      const page = (await response.json()) as PublicEventPage;
      setEvents((current) => [...current, ...page.events]);
      setCursor(page.next_cursor);
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }

  if (events.length === 0) {
    return (
      <EmptyState title={t("emptyTitle")} description={t("emptyDescription")} />
    );
  }

  return (
    <div className="space-y-6">
      <EventGrid events={events} />
      {failed ? (
        <p role="alert" className="text-center text-sm text-destructive">
          {t("loadMoreFailed")}
        </p>
      ) : null}
      {cursor ? (
        <div className="flex justify-center">
          <Button variant="secondary" onClick={loadMore} disabled={loading} aria-busy={loading}>
            {loading ? t("loadingMore") : t("loadMore")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
