import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Alert, AlertDescription, AlertTitle, Badge, Breadcrumb, StorefrontShell } from "@ticket-pos/ui";

import { TicketTypeCard } from "@/components/ticket-type-card";
import { getPublicEvent } from "@/lib/api";
import { formatEventDateTime } from "@/lib/format";

export const dynamic = "force-dynamic";

type EventPageProps = {
  params: Promise<{ orgSlug: string; eventSlug: string }>;
};

export async function generateMetadata({ params }: EventPageProps): Promise<Metadata> {
  const { orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);
  if (!event) return { title: "Event not found" };

  const title = `${event.name} · ${event.organization.name}`;
  const description = event.description ?? `Get tickets for ${event.name}.`;
  return {
    title,
    description,
    alternates: { canonical: `/${orgSlug}/events/${eventSlug}` },
    openGraph: {
      title,
      description,
      type: "website",
      images: event.cover_image_url ? [{ url: event.cover_image_url }] : undefined,
    },
  };
}

export default async function EventPage({ params }: EventPageProps) {
  const { orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);

  if (!event) {
    notFound();
  }

  const dateLabel = formatEventDateTime(event.starts_at, event.timezone);

  return (
    <StorefrontShell
      organizationName={event.organization.name}
      organizationLogoUrl={event.organization.logo_url}
    >
      <article className="mx-auto w-full max-w-3xl px-4 py-8 sm:py-10">
        <Breadcrumb
          className="mb-6"
          items={[
            { label: "Discover events", href: "/" },
            { label: event.organization.name, href: `/${event.organization.slug}` },
            { label: event.name },
          ]}
        />
        <div className="overflow-hidden rounded-xl border bg-muted">
          <div className="relative aspect-[16/9] w-full">
            {event.cover_image_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={event.cover_image_url} alt="" className="h-full w-full object-cover" />
            ) : (
              <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-primary/10 to-primary/25">
                <span className="text-4xl font-semibold text-primary/70">
                  {event.name.charAt(0).toUpperCase()}
                </span>
              </div>
            )}
          </div>
        </div>

        <header className="mt-6 space-y-2">
          {event.has_ended ? <Badge variant="secondary">This event has ended</Badge> : null}
          <h1 className="text-3xl font-semibold tracking-tight">{event.name}</h1>
          {dateLabel ? <p className="text-muted-foreground">{dateLabel}</p> : null}
          {event.venue_name ? (
            <p className="text-muted-foreground">
              {event.venue_name}
              {event.venue_address ? ` · ${event.venue_address}` : ""}
            </p>
          ) : null}
          <p className="text-sm text-muted-foreground">
            Presented by{" "}
            <Link
              href={`/${event.organization.slug}`}
              className="font-medium text-foreground underline-offset-4 hover:underline"
            >
              {event.organization.name}
            </Link>
          </p>
        </header>

        {event.description ? (
          <div className="mt-6 whitespace-pre-line leading-relaxed text-foreground">
            {event.description}
          </div>
        ) : null}

        <section className="mt-8 space-y-4" aria-labelledby="tickets-heading">
          <h2 id="tickets-heading" className="text-lg font-semibold tracking-tight">
            Tickets
          </h2>
          <div className="space-y-3">
            {event.ticket_types.map((ticketType) => (
              <TicketTypeCard key={ticketType.name} ticketType={ticketType} />
            ))}
          </div>

          {!event.has_ended ? (
            <Alert>
              <AlertTitle>Checkout coming soon</AlertTitle>
              <AlertDescription>
                Ticket sales for this event are not open yet. Please check back shortly.
              </AlertDescription>
            </Alert>
          ) : null}
        </section>
      </article>
    </StorefrontShell>
  );
}
