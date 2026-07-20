"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

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

import {
  type EventListItem,
  fetchEventsJSON,
  formatEventStartDate,
  statusBadgeVariant,
} from "@/lib/events-api";

import { DiscoverabilityToggle } from "./discoverability-toggle";

type EventsPageClientProps = {
  isOrgAdmin: boolean;
};

export function EventsPageClient({ isOrgAdmin }: EventsPageClientProps) {
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
      const message = loadError instanceof Error ? loadError.message : "Failed to load events";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadEvents();
  }, [loadEvents]);

  const handleDiscoverableChange = useCallback((eventId: string, discoverable: boolean) => {
    setEvents((current) =>
      current.map((event) => (event.id === eventId ? { ...event, discoverable } : event)),
    );
  }, []);

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading events...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>You do not have access to these events.</AlertDescription>
      </Alert>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load events</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Events"
        description={
          isOrgAdmin
            ? "Create and manage Events and Ticket Types."
            : "Choose which Events are listed on the public storefront."
        }
        actions={
          isOrgAdmin ? (
            <Button asChild>
              <Link href="/events/new">Create event</Link>
            </Button>
          ) : undefined
        }
      />

      {events.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-start gap-4 py-10">
            {isOrgAdmin ? (
              <>
                <p className="text-muted-foreground">
                  No events yet. Create your first Event to start building your catalog.
                </p>
                <Button asChild>
                  <Link href="/events/new">Create event</Link>
                </Button>
              </>
            ) : (
              <p className="text-muted-foreground">No events yet.</p>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {events.map((event) => {
            const details = (
              <div className="space-y-1">
                <p className="font-medium">{event.name}</p>
                <p className="text-sm text-muted-foreground">
                  {formatEventStartDate(event.starts_at, event.timezone)}
                </p>
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
                  <Badge variant={statusBadgeVariant(event.status)} className="w-fit capitalize">
                    {event.status}
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
