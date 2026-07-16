import type { PublicEventCard } from "@/lib/api";

import { EventCard } from "./event-card";

type EventGridProps = {
  events: PublicEventCard[];
  showOrganization?: boolean;
};

export function EventGrid({ events, showOrganization = true }: EventGridProps) {
  return (
    <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
      {events.map((event) => (
        <EventCard
          key={`${event.organization.slug}/${event.slug}`}
          event={event}
          showOrganization={showOrganization}
        />
      ))}
    </div>
  );
}

type EmptyStateProps = {
  title: string;
  description: string;
};

export function EmptyState({ title, description }: EmptyStateProps) {
  return (
    <div className="rounded-lg border border-dashed p-10 text-center">
      <p className="font-medium">{title}</p>
      <p className="mt-1 text-sm text-muted-foreground">{description}</p>
    </div>
  );
}
