import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { useFormatLocale } from "@/i18n/format-locale";
import type { PublicTicketType } from "@/lib/api";
import { formatSalesCutoff, ticketTypeBadge } from "@/lib/sales-cutoff";

/**
 * How a Sales Cutoff looks on the Storefront: the one badge beside a Ticket
 * Type's name — counting the last week down while it is still selling, saying
 * "Sales closed" once the door has shut — and the line under it saying when that
 * was (ADR 0070).
 *
 * The badge outgrew the cutoff and now carries sold out and limit reached too,
 * because those are the states it has to beat: one card says one thing about
 * itself, and the only way to guarantee that is for one place to choose. Ranking
 * them elsewhere and drawing them here would let a fourth badge reappear on one
 * list and not the other, which is exactly what this replaced.
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
 * The ONE badge beside a Ticket Type's name: the state it is in, or — on a card
 * in no state at all — how many days are left to buy it (ADR 0070).
 *
 * Which one is lib/sales-cutoff.ts's to decide; this is the lookup that turns its
 * answer into words and a colour. Both Ticket Type lists render it, so a closed
 * Ticket Type cannot be badged one way on the sellable list and another on the
 * read-only one, and neither list can quietly regrow a second badge.
 *
 * Two colours and never three. Sold out, closed and limit reached are statements
 * of fact and wear `secondary` and `outline` as they always have; the countdown
 * is the only thing on the card asking a Customer to hurry, and it wears
 * `warning` — amber. Never `destructive`: red means FAILURE in both apps, and
 * teaching buyers that it also means hurry spends the one meaning it has. The
 * escalation from "7 days left" to "Closes today" is carried by the words.
 *
 * Every arm is ordinary visible text, so a screen reader is told the state and
 * the deadline in words and colour is never the only signal.
 */
export function TicketTypeStateBadge({
  ticketType,
  limitReached,
  timezone,
  now,
}: {
  ticketType: PublicTicketType;
  /** Whether this reader has spent their own Purchase Limit on it (ADR 0025). */
  limitReached?: boolean;
  /** The Event's timezone; the cutoff is set in it and counted down on it. */
  timezone?: string | null;
  /**
   * The clock to count down from, omitted on a surface that never counts down.
   *
   * The SERVER's instant, read once with the page and handed the whole list, so
   * every card counts off the same moment and no card's rung can differ between
   * what was server-rendered and what hydration draws (ADR 0070).
   */
  now?: Date | null;
}) {
  const t = useTranslations("event");
  const badge = ticketTypeBadge(ticketType, { limitReached, timezone, now });
  if (!badge) return null;

  switch (badge.kind) {
    case "sold_out":
      return <Badge variant="secondary">{t("soldOut")}</Badge>;
    case "closed":
      return <Badge variant="secondary">{t("salesClosed")}</Badge>;
    case "limit_reached":
      return <Badge variant="outline">{t("limitReached")}</Badge>;
    case "closes_today":
      return <Badge variant="warning">{t("salesCloseToday")}</Badge>;
    case "closes_tomorrow":
      return <Badge variant="warning">{t("salesCloseTomorrow")}</Badge>;
    case "days_left":
      return <Badge variant="warning">{t("salesDaysLeft", { count: badge.days })}</Badge>;
  }
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
