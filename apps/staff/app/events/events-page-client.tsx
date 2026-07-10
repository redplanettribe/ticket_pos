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

export function EventsPageClient() {
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

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading events...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>You need Org Admin access to manage the event catalog.</AlertDescription>
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
        description="Create and manage Events and Ticket Types."
        actions={
          <Button asChild>
            <Link href="/events/new">Create event</Link>
          </Button>
        }
      />

      {events.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-start gap-4 py-10">
            <p className="text-muted-foreground">No events yet. Create your first Event to start building your catalog.</p>
            <Button asChild>
              <Link href="/events/new">Create event</Link>
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {events.map((event) => (
            <Link
              key={event.id}
              href={`/events/${event.id}`}
              className="flex flex-col gap-3 rounded-md border p-4 transition-colors hover:bg-muted/50 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="space-y-1">
                <p className="font-medium">{event.name}</p>
                <p className="text-sm text-muted-foreground">{formatEventStartDate(event.starts_at, event.timezone)}</p>
              </div>
              <Badge variant={statusBadgeVariant(event.status)} className="w-fit capitalize">
                {event.status}
              </Badge>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
