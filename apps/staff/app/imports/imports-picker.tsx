"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Card,
  CardContent,
} from "@ticket-pos/ui";

import {
  type EventListItem,
  fetchEventsJSON,
  formatEventStartDate,
  statusBadgeVariant,
} from "@/lib/events-api";

// ImportsPicker is the entry point to the per-event Sale Import flow: pick an
// Event and open its detail, where the "Import sales" surface lives.
export function ImportsPicker() {
  const [events, setEvents] = useState<EventListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadEvents = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchEventsJSON<EventListItem[]>("/api/events");
      setEvents(data);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "Failed to load events");
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

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load events</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  if (events.length === 0) {
    return (
      <Card>
        <CardContent className="py-10">
          <p className="text-muted-foreground">
            No events yet. Create an Event before importing sales.
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-3">
      {events.map((event) => (
        <Link
          key={event.id}
          href={`/events/${event.id}#import-sales`}
          className="flex flex-col gap-3 rounded-md border p-4 transition-colors hover:bg-muted/50 sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="space-y-1">
            <p className="font-medium">{event.name}</p>
            <p className="text-sm text-muted-foreground">
              {formatEventStartDate(event.starts_at, event.timezone)}
            </p>
          </div>
          <Badge variant={statusBadgeVariant(event.status)} className="w-fit capitalize">
            {event.status}
          </Badge>
        </Link>
      ))}
    </div>
  );
}
