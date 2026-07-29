import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { after } from "next/server";

import { Badge, Breadcrumb, StorefrontShell } from "@ticket-pos/ui";

import { EventHeroMedia } from "@/components/event-hero-media";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { TicketSelection } from "@/components/ticket-selection";
import { TicketTypeCard } from "@/components/ticket-type-card";
import { affiliateCodeFromRef, recordAffiliateClick } from "@/lib/affiliate-click";
import { getPublicEvent } from "@/lib/api";
import { formatEventDateTime } from "@/lib/format";

export const dynamic = "force-dynamic";

type EventPageProps = {
  params: Promise<{ orgSlug: string; eventSlug: string }>;
  /** `?ref=CODE` — the Affiliate Link this page was reached through, if any. */
  searchParams: Promise<{ ref?: string | string[] }>;
};

export async function generateMetadata({ params }: EventPageProps): Promise<Metadata> {
  const { orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);
  if (!event) return { title: "Event not found" };

  const title = `${event.name} · ${event.organization.name}`;
  const description = event.description ?? `Get tickets for ${event.name}.`;
  const path = `/${orgSlug}/events/${eventSlug}`;
  // Cover URLs are already absolute (object storage), so previews render even
  // when metadataBase is unset off-platform.
  const images = event.cover_image_url ? [event.cover_image_url] : undefined;
  return {
    title,
    description,
    alternates: { canonical: path },
    openGraph: {
      title,
      description,
      type: "website",
      url: path,
      images,
    },
    twitter: {
      card: "summary_large_image",
      title,
      description,
      images,
    },
  };
}

export default async function EventPage({ params, searchParams }: EventPageProps) {
  const { orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);

  if (!event) {
    notFound();
  }

  // The Affiliate Link this visitor arrived through, counted after the response
  // is on its way: the click is display-only stats for an organizer, so it may
  // never sit between a buyer and the page. The code is not checked first — the
  // API accepts and ignores a dead one, and asking would be a round trip spent
  // on nothing.
  const code = affiliateCodeFromRef((await searchParams).ref);
  if (code) {
    after(() => recordAffiliateClick(orgSlug, eventSlug, code));
  }

  const dateLabel = formatEventDateTime(event.starts_at, event.timezone);

  return (
    <StorefrontShell
      organizationName={event.organization.name}
      organizationLogoUrl={event.organization.logo_url}
      customerNav={<HeaderCustomerNav />}
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
              // The Cover Image is the hero and renders from the server every
              // time; the Cover Video, when there is one and the visitor has not
              // declined motion or data, fades in over it from the client
              // (ADR 0020).
              <EventHeroMedia
                coverImageUrl={event.cover_image_url}
                coverVideoUrl={event.cover_video_url}
              />
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
          {event.tags.length > 0 ? (
            <div className="flex flex-wrap gap-1.5 pt-1">
              {event.tags.map((tag) => (
                <Badge key={tag.name} variant="outline">
                  {tag.name}
                </Badge>
              ))}
            </div>
          ) : null}
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
          {event.has_ended ? (
            // An ended Event stays reachable but is no longer sellable: the
            // Ticket Types render read-only, with no steppers and no checkout.
            <div className="space-y-3">
              {event.ticket_types.map((ticketType) => (
                <TicketTypeCard
                  key={ticketType.name}
                  ticketType={ticketType}
                  timezone={event.timezone}
                />
              ))}
              {event.price_includes_fee ? (
                <p className="text-xs text-muted-foreground">Prices include the service fee.</p>
              ) : null}
            </div>
          ) : (
            // Draft and cancelled Events never reach this page at all — the
            // public event read only acknowledges published Events — so the
            // purchase UI only ever exists where selling is allowed.
            <TicketSelection
              orgSlug={event.organization.slug}
              eventSlug={event.slug}
              eventName={event.name}
              ticketTypes={event.ticket_types}
              priceIncludesFee={event.price_includes_fee}
              timezone={event.timezone}
            />
          )}
        </section>
      </article>
    </StorefrontShell>
  );
}
