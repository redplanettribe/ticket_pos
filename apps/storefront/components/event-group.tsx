import { getTranslations } from "next-intl/server";

import { HeldTicketRows } from "@/components/held-ticket-rows";
import { TicketSaleCard } from "@/components/ticket-sale-card";
import { getFormatLocale } from "@/i18n/format-locale.server";
import { Link } from "@/i18n/navigation";
import type { EventGroup as EventGroupData } from "@/lib/customer-area-tabs";
import { formatEventDateTime } from "@/lib/format";

/**
 * One card per Event in the Customer Area (#354): the Event's name, when and
 * where, the Organization presenting it, and then one row per Ticket Sale the
 * Customer made for it, followed by one row per Ticket somebody else bought
 * for it and gave this Customer (#356).
 *
 * The Event is the thing the Customer came for, so it is the heading; the
 * Sales sit inside it because the money, the Sale Confirmation, the Tax ID
 * and the Reversal Window are each facts about one purchase and stay with it.
 * Before this the page drew a full card per Sale, and a buyer who had bought
 * six times for one conference read the same Event name six times at the
 * same weight as everything else.
 *
 * A held Ticket sits in the same list because it is a ticket to this Event
 * whoever paid; it is drawn by a client piece, because its questions are
 * fetched and answered from the browser, and it shows none of what a Sale
 * row shows about the purchase — see components/held-ticket-rows.tsx.
 */
export async function EventGroup({
  group,
  badgeReversed = true,
  viaConfirmationLink = false,
  customerEmail = null,
}: {
  /** Passed through to each Sale row; false on the Reversed tab. */
  badgeReversed?: boolean;
  group: EventGroupData;
  /** See TicketSaleCard: true for a reader who arrived by a Confirmation Link. */
  viaConfirmationLink?: boolean;
  /** The address this session belongs to, to prefill sign-in with. */
  customerEmail?: string | null;
}) {
  const { event, organization, sales, held } = group;
  const t = await getTranslations("customerArea");
  // The Event page's own words for the Organization behind an Event, read from
  // where they are written rather than restated here: a purchase and the Event
  // it is for must not credit the same Organization two different ways.
  const eventCopy = await getTranslations("event");
  const formatLocale = await getFormatLocale();
  // The words of a date are the Customer's language; the clock behind them
  // stays the Event's own timezone, which is a fact about the Event.
  const dateLabel = formatEventDateTime(event.starts_at, event.timezone, formatLocale);
  const when = dateLabel ?? t("dateTbd");
  const eventHref = `/${organization.slug}/events/${event.slug}`;

  return (
    <li className="rounded-lg border bg-card p-5 sm:p-6">
      <div className="flex flex-col gap-1">
        <h3 className="text-lg font-semibold tracking-tight">
          <Link href={eventHref} className="hover:text-primary hover:underline">
            {event.name}
          </Link>
        </h3>
        <p className="text-sm text-muted-foreground">
          {/* Two facts joined by a separator we chose, so the join is a message:
              a language that wants a comma, or the venue first, can have it. */}
          {event.venue_name ? t("eventWhenWhere", { when, venue: event.venue_name }) : when}
        </p>
        <p className="text-sm text-muted-foreground">
          {eventCopy.rich("presentedBy", {
            organization: organization.name,
            organizer: (chunks) => (
              <Link href={`/${organization.slug}`} className="hover:text-foreground hover:underline">
                {chunks}
              </Link>
            ),
          })}
        </p>
      </div>

      <ul className="mt-4 divide-y border-t">
        {sales.map((sale) => (
          <TicketSaleCard
            badgeReversed={badgeReversed}
            key={sale.id}
            sale={sale}
            // A person with one ticket to this Event should never have to
            // click to see it; with several, the collapsed rows are the
            // overview and each opens on demand.
            defaultOpen={sales.length + held.length === 1}
            viaConfirmationLink={viaConfirmationLink}
            customerEmail={customerEmail}
          />
        ))}
        {/* After the Sales: what the Customer bought comes before what they
            were given, and a buyer with no purchases for this Event sees
            the held rows alone. */}
        {held.length > 0 ? (
          <HeldTicketRows held={held} defaultOpen={sales.length + held.length === 1} />
        ) : null}
      </ul>
    </li>
  );
}
