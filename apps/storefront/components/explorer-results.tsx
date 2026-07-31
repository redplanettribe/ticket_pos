"use client";

import { useTranslations } from "next-intl";
import { useRef, useState } from "react";

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

  /**
   * Which filter selection the state above belongs to.
   *
   * The filters live in the URL, so applying one is a navigation: the server
   * re-renders the page and hands this component a fresh first page of results.
   * But it is the *same* component in the same slot of the tree, so React
   * re-renders it in place rather than remounting it — and a useState
   * initializer only runs on mount. Without the reset below, `events` and
   * `cursor` keep the previous filter's page forever: the chips light up, the
   * URL changes, the server does the right query, and the grid never moves.
   *
   * The signature is compared by value because `tags` is a fresh array on every
   * render. This is React's documented "adjust state when props change" pattern
   * — a render-time set, not an effect, so no filtered page is ever painted
   * showing the previous filter's results.
   *
   * This lives here rather than as a `key` at the call site for the same reason
   * the sitemap's cache opt-in lives in listPublicEvents: a call site that
   * forgets it reintroduces a bug whose only symptom is a stale grid.
   */
  const filterKey = JSON.stringify([q, from, to, tags ?? []]);
  const [appliedKey, setAppliedKey] = useState(filterKey);
  // Read by in-flight "Load more" responses to tell whether the filters moved
  // while they were on the wire.
  const latestKey = useRef(filterKey);
  latestKey.current = filterKey;

  if (appliedKey !== filterKey) {
    setAppliedKey(filterKey);
    setEvents(initialEvents);
    setCursor(initialCursor);
    setFailed(false);
    setLoading(false);
  }

  async function loadMore() {
    if (!cursor) return;
    const requestKey = filterKey;
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
      // The filters changed while this page was on the wire, so it answers a
      // question nobody is asking any more. Appending it would splice the old
      // filter's Events onto the new filter's grid.
      if (latestKey.current !== requestKey) return;
      setEvents((current) => [...current, ...page.events]);
      setCursor(page.next_cursor);
    } catch {
      if (latestKey.current !== requestKey) return;
      setFailed(true);
    } finally {
      if (latestKey.current === requestKey) setLoading(false);
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
