"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  PageHeader,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import {
  ApiError,
  eventStatusKey,
  fetchEventsJSON,
  statusBadgeVariant,
  type EventListItem,
} from "@/lib/events-api";
import {
  eventsEmptyState,
  eventsTabCounts,
  partitionEvents,
  type EventsTabKey,
} from "@/lib/events-tabs";
import { formatDateTime } from "@/lib/format";

import { DiscoverabilityToggle } from "./discoverability-toggle";
import { EventsTabs } from "./events-tabs";

type EventsPageClientProps = {
  isOrgAdmin: boolean;
  /** Which of the three tabs this route is, handed down from its route file. */
  tab: EventsTabKey;
};

/**
 * The Events list, on whichever of its three tabs is being read (#613).
 *
 * One component behind all three routes, handed the tab it should show: the
 * fetch and the partition happen once here rather than three times, and the
 * whole unfiltered payload is what the split is done over — the list endpoint
 * gains no parameter for this (ADR 0071).
 */
export function EventsPageClient({ isOrgAdmin, tab }: EventsPageClientProps) {
  const t = useTranslations("events");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [events, setEvents] = useState<EventListItem[]>([]);
  // The clock, read ONCE and held (ADR 0071). Not `new Date()` inside the memo
  // below: that would be re-read every time the list changes — when a Listed
  // toggle rewrites one row, say — and an Event could cross its end instant
  // between two renders and jump tabs under the reader's cursor. A page left
  // open overnight shows yesterday's Event in Active until something reloads,
  // which is deliberate and must not be "fixed" with a timer.
  const [now] = useState(() => new Date());
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadEvents = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchEventsJSON<EventListItem[]>("/api/events");
      setEvents(data);
      setForbidden(false);
    } catch (loadError) {
      // The API's own code, not a substring of its English sentence. This used
      // to read `message.includes("permission")`, which stopped being a test of
      // anything the moment the sentence could arrive in Spanish (ADR 0023).
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
        return;
      }
      setError(
        apiErrorMessage(errorCopy, loadError instanceof ApiError ? loadError : null) ??
          t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [errorCopy, t]);

  useEffect(() => {
    void loadEvents();
  }, [loadEvents]);

  // The whole payload split into its three tabs, of which this route draws one
  // and the strip above it counts all three. One partition feeds both, so the
  // number on a tab and the rows it leads to are the same arrays read twice and
  // cannot drift apart. Both are ready before this render returns: the counts
  // are correct on the first paint that has any Events to count, with no second
  // render and no loading state of their own (#614).
  const tabs = useMemo(() => partitionEvents(events, now), [events, now]);
  const counts = useMemo(() => eventsTabCounts(tabs), [tabs]);
  const rows = tabs[tab];
  // What this tab says when it is empty, decided over the whole payload and not
  // over this tab's slice: `firstRun` is the one moment an invitation to create
  // an Event is a true sentence (#615).
  const emptyState = eventsEmptyState(tab, events.length > 0);

  const handleDiscoverableChange = useCallback((eventId: string, discoverable: boolean) => {
    setEvents((current) =>
      current.map((event) => (event.id === eventId ? { ...event, discoverable } : event)),
    );
  }, []);

  if (loading) {
    // The strip is drawn while the list is still coming: each tab is a route of
    // its own, and a reader who wants Past should not have to wait for Active
    // to arrive before they can ask for it. The refusals below keep their bare
    // shape — every tab is the same one payload, so a strip over a load that
    // failed or was refused offers three doors into the same wall.
    // No counts are handed over here, and deliberately not zeros — see the
    // `counts` prop on `EventsTabs` for why (#614).
    return (
      <div className="space-y-6">
        <EventsTabs counts={null} />
        <p className="text-sm text-muted-foreground">{t("loading")}</p>
      </div>
    );
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDeniedDescription")}</AlertDescription>
      </Alert>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("title")}
        description={isOrgAdmin ? t("descriptionOrgAdmin") : t("descriptionMember")}
        actions={
          isOrgAdmin ? (
            <Button asChild>
              <Link href="/events/new">{t("createEvent")}</Link>
            </Button>
          ) : undefined
        }
      />

      {/* Always all three, even when this one is empty: the tabs are a fixed
          partition of the Organization's Events and not a set that shrinks. An
          empty tab reads zero rather than going bare, which is the whole point
          — a reader should be able to tell an empty Cancelled from a full one
          without opening it. */}
      <EventsTabs counts={counts} />

      {/* An empty tab says something true (#615). The first-run invitation —
          "create your first Event" — is only ever shown to an Organization
          that has no Events at all, and only on the tab it lands on; it keeps
          its role split and its button, because varying a prompt is the whole
          reason that split exists. Every other empty tab states plainly that it
          is empty and asks for nothing: an Org Admin whose Events are all
          cancelled does not need to be told to start building a catalog.

          Either way this is a card with a sentence in it, drawn only after the
          load has succeeded — the loading state above is a bare line of text
          and a failure is a destructive Alert, so none of the three can be
          taken for either. The Create event button stays in the page header on
          all three tabs regardless: it is a page action, and hiding it on Past
          would be a puzzle. */}
      {rows.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-start gap-4 py-10">
            {emptyState !== "firstRun" ? (
              <p className="text-muted-foreground">{t(emptyState)}</p>
            ) : isOrgAdmin ? (
              <>
                <p className="text-muted-foreground">{t("emptyOrgAdmin")}</p>
                <Button asChild>
                  <Link href="/events/new">{t("createEvent")}</Link>
                </Button>
              </>
            ) : (
              <p className="text-muted-foreground">{t("empty")}</p>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {rows.map((event) => {
            // The Event's own timezone, whichever language the reader is in: a
            // locale decides the marks around a date and nothing about which
            // clock it is on (ADR 0041). An Event that has not picked one yet
            // has no moment to draw, so nothing is drawn.
            const startsAt =
              event.timezone && formatDateTime(event.starts_at, event.timezone, locale);
            const status = eventStatusKey(event.status);
            const details = (
              <div className="space-y-1">
                <p className="font-medium">{event.name}</p>
                <p className="text-sm text-muted-foreground">{startsAt || t("noDateSet")}</p>
              </div>
            );

            return (
              <div
                key={event.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                {isOrgAdmin ? (
                  <Link
                    href={`/events/${event.id}`}
                    className="-m-2 rounded-md p-2 transition-colors hover:bg-muted/50"
                  >
                    {details}
                  </Link>
                ) : (
                  details
                )}
                <div className="flex items-center gap-3">
                  <Badge variant={statusBadgeVariant(event.status)} className="w-fit">
                    {status ? t(status) : event.status}
                  </Badge>
                  <DiscoverabilityToggle
                    eventId={event.id}
                    status={event.status}
                    discoverable={event.discoverable}
                    onChange={(discoverable) => handleDiscoverableChange(event.id, discoverable)}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
