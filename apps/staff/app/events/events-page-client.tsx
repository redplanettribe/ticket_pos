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
import { partitionEvents, type EventsTabKey } from "@/lib/events-tabs";
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

  // The whole payload split into its three tabs, of which this route draws one.
  const rows = useMemo(() => partitionEvents(events, now)[tab], [events, now, tab]);

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
    return (
      <div className="space-y-6">
        <EventsTabs />
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
          partition of the Organization's Events and not a set that shrinks. */}
      <EventsTabs />

      {/* The existing first-run copy, now shown when THIS tab has no rows. It
          reads oddly on an empty Past tab belonging to an Organization with
          twenty Events; telling the two apart is #615, deliberately not here. */}
      {rows.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-start gap-4 py-10">
            {isOrgAdmin ? (
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
