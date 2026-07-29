import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Badge, Breadcrumb } from "@ticket-pos/ui";

import { EventHeroMedia } from "@/components/event-hero-media";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { TicketSelection } from "@/components/ticket-selection";
import { TicketTypeCard } from "@/components/ticket-type-card";
import { Link } from "@/i18n/navigation";
import { localeAlternates } from "@/lib/alternates";
import { getPublicEvent } from "@/lib/api";
import { formatEventDateTime } from "@/lib/format";
import { localizedPath, toAppLocale } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";

export const dynamic = "force-dynamic";

type EventPageProps = {
  params: Promise<{ locale: string; orgSlug: string; eventSlug: string }>;
};

export async function generateMetadata({ params }: EventPageProps): Promise<Metadata> {
  const { locale, orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);
  if (!event) return { title: "Event not found" };

  const title = `${event.name} · ${event.organization.name}`;
  const description = event.description ?? `Get tickets for ${event.name}.`;
  // The canonical address is the one being served, locale and all: /en and /es
  // are two pages, and a canonical that named neither would ask a crawler to
  // pick one for us. The languages map pairs them (lib/alternates.ts).
  const { canonical, languages } = localeAlternates(
    `/${orgSlug}/events/${eventSlug}`,
    toAppLocale(locale),
    storefrontBaseUrl(),
  );
  // Cover URLs are already absolute (object storage), so previews render even
  // when metadataBase is unset off-platform.
  const images = event.cover_image_url ? [event.cover_image_url] : undefined;
  return {
    title,
    description,
    alternates: { canonical, languages },
    openGraph: {
      title,
      description,
      type: "website",
      // The same address the canonical names, so a share from the Spanish page
      // opens the Spanish page.
      url: canonical,
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

export default async function EventPage({ params }: EventPageProps) {
  const { locale, orgSlug, eventSlug } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const appLocale = toAppLocale(locale);
  const event = await getPublicEvent(orgSlug, eventSlug);

  if (!event) {
    notFound();
  }

  const dateLabel = formatEventDateTime(event.starts_at, event.timezone);
  const t = await getTranslations("shell");

  return (
    <StorefrontShell
      organizationName={event.organization.name}
      organizationLogoUrl={event.organization.logo_url}
      customerNav={<HeaderCustomerNav />}
    >
      <article className="mx-auto w-full max-w-3xl px-4 py-8 sm:py-10">
        <Breadcrumb
          className="mb-6"
          label={t("breadcrumbLabel")}
          items={[
            // Plain anchors in the shared UI package, so these carry the
            // locale explicitly rather than through the navigation helpers.
            { label: "Discover events", href: localizedPath(appLocale, "/") },
            {
              label: event.organization.name,
              href: localizedPath(appLocale, `/${event.organization.slug}`),
            },
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
