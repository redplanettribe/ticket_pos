import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";
import { after } from "next/server";

import { Badge, Breadcrumb, Markdown } from "@ticket-pos/ui";

import { EventHeroMedia } from "@/components/event-hero-media";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { RegisterPanel } from "@/components/register-panel";
import { StorefrontShell } from "@/components/storefront-shell";
import { FollowableTags } from "@/components/followable-tags";
import { TicketSelection } from "@/components/ticket-selection";
import { TicketTypeCard } from "@/components/ticket-type-card";
import { getFormatLocale } from "@/i18n/format-locale.server";
import { Link } from "@/i18n/navigation";
import { affiliateCodeFromRef, recordAffiliateClick } from "@/lib/affiliate-click";
import { localeAlternates } from "@/lib/alternates";
import { getPrivacyPolicy, getPublicEvent } from "@/lib/api";
import { checkoutIdentity } from "@/lib/checkout-identity";
import { opensCheckout } from "@/lib/checkout-signin";
import { customerSessionToken, getCustomerSession, getFollows } from "@/lib/customer-session";
import { formatEventDateTime } from "@/lib/format";
import { localizedPath, toAppLocale } from "@/lib/locale";
import { markdownSummary } from "@/lib/markdown-summary";
import { isExternallyRegistered } from "@/lib/registration";
import { restoreSelectionFromParam } from "@/lib/selection-url";
import { storefrontBaseUrl } from "@/lib/site";
import { toTagTranslator } from "@/lib/tag-name";
import { whatsappLink } from "@/lib/whatsapp";

export const dynamic = "force-dynamic";

type EventPageProps = {
  params: Promise<{ locale: string; orgSlug: string; eventSlug: string }>;
  /**
   * `?ref=CODE` — the Affiliate Link this page was reached through, if any.
   *
   * `?sel=…` — the ticket selection the buyer had chosen before being sent to
   * sign in, if any (ADR 0054, lib/selection-url.ts). It is a suggestion and
   * nothing more: it is re-judged below against the Event as it is now.
   *
   * `?checkout=1` — the buyer came back through the wall at Buy and was on
   * their way to pay, so the dialog opens on arrival rather than making them
   * press the same button twice (lib/checkout-signin.ts). A request and not a
   * grant: it opens nothing for a visitor with no Customer Session and nothing
   * for an empty basket.
   */
  searchParams: Promise<{
    ref?: string | string[];
    sel?: string | string[];
    checkout?: string | string[];
  }>;
};

export async function generateMetadata({ params }: EventPageProps): Promise<Metadata> {
  const { locale, orgSlug, eventSlug } = await params;
  const event = await getPublicEvent(orgSlug, eventSlug);
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "event" });
  if (!event) return { title: t("notFoundTitle") };

  // The Event's and the Organization's names are theirs, and the Event's own
  // description is the one the organizer wrote: none of the three is translated.
  // Only the frame around them is.
  const title = t("metaTitle", { event: event.name, organization: event.organization.name });
  // A preview is plain text wherever it lands, so the description is read for
  // its words and its Markdown is left behind (lib/markdown-summary.ts).
  const description =
    (event.description ? markdownSummary(event.description) : null) ??
    t("metaDescription", { event: event.name });
  // Cover URLs are already absolute (object storage), so previews render even
  // when metadataBase is unset off-platform.
  const images = event.cover_image_url ? [event.cover_image_url] : undefined;

  // A non-Discoverable Event is kept out of the index, and is otherwise
  // untouched.
  //
  // ADR 0002 splits reachability from discoverability: a published Event is
  // always loadable by direct link, and Discoverable only decides whether the
  // platform's own surfaces advertise it. That line was drawn for people and
  // never for crawlers, so an unlisted Event whose URL leaked once — a forward,
  // a referrer, a link in a public thread — ends up advertising itself from the
  // search results, which is the one thing the organizer switched off. `follow`
  // stays on: the pages this one links to are public and are not being hidden.
  //
  // It gets no canonical and no hreflang set, because both are instructions
  // about how to index a page: which address to file it under, and which
  // translation to file beside it. Stated on a page asking not to be filed at
  // all, they are at best noise and at worst an invitation to reconcile the
  // contradiction the other way — indexing the Spanish twin that the annotation
  // itself just pointed at. og:url goes with them; the share preview needs
  // nothing but the title, the words and the picture.
  if (!event.discoverable) {
    return {
      title,
      description,
      robots: { index: false, follow: true },
      openGraph: { title, description, type: "website", images },
      twitter: { card: "summary_large_image", title, description, images },
    };
  }

  // The canonical address is the one being served, locale and all: /en and /es
  // are two pages, and a canonical that named neither would ask a crawler to
  // pick one for us. The languages map pairs them (lib/alternates.ts).
  const { canonical, languages } = localeAlternates(
    `/${orgSlug}/events/${eventSlug}`,
    toAppLocale(locale),
    storefrontBaseUrl(),
  );
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

export default async function EventPage({ params, searchParams }: EventPageProps) {
  const { locale, orgSlug, eventSlug } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const appLocale = toAppLocale(locale);
  // Read as whoever is signed in, when somebody is. The token adds nothing but
  // each Ticket Type's already_held — what THIS Customer already holds — which
  // is what lets the steppers offer a real remaining allowance instead of the
  // whole Purchase Limit over again, and lets a Ticket Type whose allowance is
  // spent say so instead of looking sold out (ADR 0025, #168).
  //
  // An anonymous visitor passes no token and sees precisely the page they saw
  // before: already_held comes back null, which means "we do not know who is
  // asking" and never 0. The route is public, so a dead or garbage token is read
  // as a guest rather than refused, and this page is force-dynamic anyway — the
  // cookie read costs no cacheability that was ever on offer.
  const event = await getPublicEvent(orgSlug, eventSlug, await customerSessionToken());

  if (!event) {
    notFound();
  }

  // The Affiliate Link this visitor arrived through, counted after the response
  // is on its way: the click is display-only stats for an organizer, so it may
  // never sit between a buyer and the page. The code is not checked first — the
  // API accepts and ignores a dead one, and asking would be a round trip spent
  // on nothing.
  //
  // The Locale the visitor is reading in is not part of it: a click is a click,
  // and the two slugs name the Event in every language.
  const query = await searchParams;
  const code = affiliateCodeFromRef(query.ref);
  if (code) {
    after(() => recordAffiliateClick(orgSlug, eventSlug, code));
  }

  // The selection this visitor arrived with, re-judged against the Ticket Types
  // that were just read — the same read that decided what the steppers may
  // offer, so the restored quantities and their bounds cannot disagree
  // (ADR 0054, ADR 0025, ADR 0021).
  //
  // Judged HERE rather than in the client component because it is a pure
  // function of data the server already holds: doing it on this side means the
  // first paint is the restored basket rather than an empty one that fills in a
  // moment later. With no `sel` at all it produces an empty selection and an
  // empty report, which is exactly the page as it was before this existed.
  const restoredSelection = restoreSelectionFromParam(event.ticket_types, query.sel);

  // WHO THIS PAGE WOULD SELL TO, which is the wall at Buy (ADR 0054, #385).
  //
  // Read here, on the server, with the page — so the Buy control knows before
  // the buyer touches it whether it is a button or a link to sign in, and so
  // the dialog it opens can state the address it is writing to in the same
  // paint. It is the same read the header already makes on this page, and an
  // anonymous visitor pays nothing for it: with no cookie, getCustomerSession
  // returns without calling the API at all.
  //
  // Null for a visitor with no session, for a Confirmation Link session — which
  // is not Proof of Email Ownership and which the API refuses to sell to (#384)
  // — and for a session read that failed. All three meet the wall, and the
  // sign-in page reads the same session and lets straight through anybody who
  // turns out to have one after all.
  const identity = checkoutIdentity(await getCustomerSession());

  // The words are the visitor's language; the clock stays the Event's own.
  const dateLabel = formatEventDateTime(event.starts_at, event.timezone, await getFormatLocale());
  const t = await getTranslations("event");
  const shell = await getTranslations("shell");
  const explorer = await getTranslations("explorer");
  // Preset Tag copy is the Storefront's, not the API's (ADR 0027).
  const tTags = toTagTranslator(await getTranslations("tags"));

  // What this Customer already Follows, read once for the whole Tag row (#218).
  // An anonymous visitor holds no cookie, so this costs them no API call at all
  // and comes back "signed-out" — which is what makes the Tags render as the
  // plain badges they were before, with no control and no invitation.
  const follows = await getFollows();

  // The Organization's Support WhatsApp number, resolved to a tappable link.
  // Absent when they published none — the API omits the key — in which case
  // nothing renders at all: no link, no placeholder, no empty state.
  //
  // The drafted message is in the CUSTOMER's locale, not the organizer's, so a
  // Spanish-speaking organizer may receive an English opener. Accepted: they
  // recognise their own Event's name whatever surrounds it, and the alternative
  // needs an Organization language preference that does not exist. What the
  // prefill buys is the thing that makes an org-level number work at all — an
  // Organization running a dozen Events off one number knows which one is being
  // asked about before reading a word.
  const supportLink = event.organization.support_whatsapp
    ? whatsappLink(
        event.organization.support_whatsapp,
        t("supportPrefill", { event: event.name }),
      )
    : null;

  return (
    <StorefrontShell
      organizationName={event.organization.name}
      organizationLogoUrl={event.organization.logo_url}
      customerNav={<HeaderCustomerNav />}
    >
      <article className="mx-auto w-full max-w-3xl px-4 py-8 sm:py-10">
        <Breadcrumb
          className="mb-6"
          label={shell("breadcrumbLabel")}
          items={[
            // Plain anchors in the shared UI package, so these carry the
            // locale explicitly rather than through the navigation helpers.
            { label: explorer("title"), href: localizedPath(appLocale, "/") },
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
          {event.has_ended ? <Badge variant="secondary">{t("ended")}</Badge> : null}
          <h1 className="text-3xl font-semibold tracking-tight">{event.name}</h1>
          {dateLabel ? <p className="text-muted-foreground">{dateLabel}</p> : null}
          {event.venue_name ? (
            <p className="text-muted-foreground">
              {event.venue_name}
              {event.venue_address ? ` · ${event.venue_address}` : ""}
            </p>
          ) : null}
          <p className="text-sm text-muted-foreground">
            {/* The link is a tag inside the sentence rather than a fragment
                glued after it: which side of the Organization's name the words
                fall on is the translator's to decide. */}
            {t.rich("presentedBy", {
              organization: event.organization.name,
              organizer: (chunks) => (
                <Link
                  href={`/${event.organization.slug}`}
                  className="font-medium text-foreground underline-offset-4 hover:underline"
                >
                  {chunks}
                </Link>
              ),
            })}
          </p>
          {/* The Tags carry the Follow control for a signed-in Customer, and are
              the plain badges they have always been for everybody else (#218). */}
          <FollowableTags tags={event.tags} t={tTags} follows={follows} className="pt-1" />
        </header>

        {event.description ? (
          <Markdown className="mt-6 text-foreground">{event.description}</Markdown>
        ) : null}

        {isExternallyRegistered(event) ? (
          // An externally registered Event sells nothing here (ADR 0028), so the
          // whole tickets section — heading, selection and sticky total — is
          // replaced rather than trimmed: a price, a stepper or a total anywhere
          // on this page would claim a sale this platform is not making.
          <RegisterPanel
            orgSlug={orgSlug}
            eventSlug={eventSlug}
            registrationUrl={event.registration_url}
            hasEnded={event.has_ended}
          />
        ) : (
          <section className="mt-8 space-y-4" aria-labelledby="tickets-heading">
            <h2 id="tickets-heading" className="text-lg font-semibold tracking-tight">
              {t("ticketsHeading")}
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
                  <p className="text-xs text-muted-foreground">{t("feeIncluded")}</p>
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
                // The basket the buyer arrived with, already re-judged. Empty
                // for everyone who arrived without one (ADR 0054).
                restoredSelection={restoredSelection}
                // Who the purchase would be addressed to, or null — the wall.
                identity={identity}
                // Whether they arrived mid-purchase. Honoured only when there
                // is somebody to sell to and something to sell them, which the
                // component decides: this is what the address ASKED for.
                openCheckoutOnArrival={opensCheckout(query.checkout)}
                buyerHoldsFirstTicket={event.buyer_holds_first_ticket}
                priceIncludesFee={event.price_includes_fee}
                timezone={event.timezone}
                // The current Policy Version's Short Notice and checkbox
                // labels, read here so the checkout dialog can show what is
                // being accepted at the moment it is accepted (#253, ADR 0036).
                //
                // Read on the SERVER with the page rather than by the dialog
                // when it opens: it is the same read the Privacy Policy page
                // makes, it costs nothing extra on a page that is already
                // dynamic, and it means the boxes are drawn with the notice
                // already in hand rather than appearing a moment after the
                // dialog does. Null when the API could not be reached, which
                // the dialog turns into an honest refusal to collect consent.
                policy={await getPrivacyPolicy(locale)}
              />
            )}
          </section>
        )}

        {/* The Organization's support line, when it published one (ADR 0029).
            Below the tickets and quiet by design: the page exists to sell, and a
            prominent support control above the fold reads as "expect problems".

            Deliberately NOT conditioned on has_ended. An ended Event's page is
            where a Customer is most likely to have a concrete grievance rather
            than a pre-sale question — a refund, a ticket that would not scan —
            and it is the page that has just taken away every other interactive
            element. Leaving support as the one thing still clickable is the
            point, and it costs no branch: this sits after the read-only Ticket
            Type list exactly as it sits after the selection UI. */}
        {supportLink ? (
          <p className="mt-8">
            <a
              href={supportLink}
              // A new tab, so a Customer who was midway through choosing tickets
              // still has the page when they come back.
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-muted-foreground underline-offset-4 hover:underline"
            >
              {/* The label names the Organization and the channel, so it needs no
                  heading above it to say what it is — it announces its own
                  purpose to a screen reader as it stands. */}
              {t("supportWhatsApp", { organization: event.organization.name })}
            </a>
          </p>
        ) : null}
      </article>
    </StorefrontShell>
  );
}
