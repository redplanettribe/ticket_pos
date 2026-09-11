import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { useFormatLocale } from "@/i18n/format-locale";
import type { PublicTicketType } from "@/lib/api";
import { formatSalesCutoff } from "@/lib/sales-cutoff";

/**
 * How a Sales Cutoff looks on the Storefront once it has passed: the "Sales
 * closed" badge beside the Ticket Type's name, and the line under it saying
 * when the door shut (ADR 0070).
 *
 * Two surfaces list Ticket Types — the sellable list with its steppers and the
 * read-only list on an Event that has ended — and they must say the same thing
 * about the same closed Ticket Type, so they take it from here rather than each
 * writing it out. The read-only card gets both pieces on purpose: the record of
 * an Event stays consistent with what a Customer saw while it was selling.
 * Splitting the pieces rather than shipping one block keeps each where its
 * surface already puts that kind of information, exactly as promotion.tsx does.
 *
 * The rest of the closed card's treatment — dimmed, no quantity stepper, no
 * remaining count — is each card withholding what it already withholds from an
 * unsellable Ticket Type, and stays where those decisions are made.
 *
 * Worded as CLOSED and never as sold out, on both surfaces. "They are gone" ends
 * the conversation and "we stopped selling" invites an email asking you to
 * reopen, and a Customer must never be told an Event is full when there are
 * forty seats left. Both pieces are ordinary visible text, so the state reaches
 * a screen reader in words and colour is never the only signal.
 *
 * Both read the `event` namespace, including inside ticket-selection.tsx: a
 * closing time is something the Event page says about a Ticket Type, and the two
 * lists must not be able to word it differently.
 *
 * A Ticket Type the server has not called closed renders exactly as it did
 * before this feature: no badge, no line (ADR 0070).
 */

/**
 * "Sales closed" — the `secondary` variant, the same one the sold-out badge
 * wears, because this is a statement of fact about the Ticket Type rather than
 * something the buyer can act on.
 *
 * Keyed on the server's verdict and never on the instant beside it: the clock is
 * the server's, and a browser set to yesterday must not be able to draw this
 * badge over a Ticket Type that is still selling.
 *
 * When a Ticket Type is both sold out and closed, both badges currently draw.
 * Ranking the states into one badge is #608's, and this module is where it will
 * go.
 */
export function SalesClosedBadge({ ticketType }: { ticketType: PublicTicketType }) {
  const t = useTranslations("event");
  if (!ticketType.closed) return null;

  return <Badge variant="secondary">{t("salesClosed")}</Badge>;
}

/**
 * "Sales closed on Thu Jul 9, 7:00 AM" — in the slot the Promotion deadline
 * uses, and on the Event's clock, so a Customer can tell whether they missed it
 * by an hour or by a month.
 *
 * Drawn only for a Ticket Type the server called closed, and only when the
 * instant is one we can actually name. A closed Ticket Type always has a cutoff,
 * so the second arm is the line going quiet rather than the badge: the state is
 * the verdict's to assert, and it stands whether or not there is a date to
 * print.
 */
export function SalesClosedLine({
  ticketType,
  timezone,
}: {
  ticketType: PublicTicketType;
  /** The Event's timezone; the cutoff is set in it (ADR 0070). */
  timezone: string | null;
}) {
  const t = useTranslations("event");
  const locale = useFormatLocale();
  const closedAt = ticketType.closed
    ? formatSalesCutoff(ticketType.sales_cutoff_at, timezone, locale)
    : null;
  if (!closedAt) return null;

  return (
    <p className="text-sm text-muted-foreground">
      {/* The emphasis is part of the sentence, so the message carries it as a
          tag: a language that puts the date first must be able to. */}
      {t.rich("salesClosedAt", {
        closedAt,
        when: (chunks) => <span className="font-medium text-foreground">{chunks}</span>,
      })}
    </p>
  );
}
