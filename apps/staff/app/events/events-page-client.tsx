"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

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
import { formatDateTime } from "@/lib/format";

import { DiscoverabilityToggle } from "./discoverability-toggle";

type EventsPageClientProps = {
  isOrgAdmin: boolean;
};

export function EventsPageClient({ isOrgAdmin }: EventsPageClientProps) {
  const t = useTranslations("events");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [events, setEvents] = useState<EventListItem[]>([]);
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

  const handleDiscoverableChange = useCallback((eventId: string, discoverable: boolean) => {
    setEvents((current) =>
      current.map((event) => (event.id === eventId ? { ...event, discoverable } : event)),
    );
  }, []);

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
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

      {events.length === 0 ? (
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
          {events.map((event) => {
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
