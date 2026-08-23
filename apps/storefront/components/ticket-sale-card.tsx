import { getTranslations } from "next-intl/server";

import { Badge } from "@ticket-pos/ui";

import { BuyerTicketAnswers } from "@/components/buyer-ticket-answers";
import { ReversalWatch } from "@/components/reversal-watch";
import { UndoPurchase } from "@/components/undo-purchase";
import { SignInToUndo, UndoWindowNotice } from "@/components/undo-window-notice";
import { getFormatLocale } from "@/i18n/format-locale.server";
import type { TicketSale } from "@/lib/customer-session";
import { ticketSaleAnchorId } from "@/lib/destination";
import { formatPrice, formatPurchaseDate } from "@/lib/format";
import { formatTaxId } from "@/lib/tax-id";
import { undoDeadline } from "@/lib/undo-window";

/**
 * One Ticket Sale, as a row inside its Event's card (components/event-group.tsx).
 *
 * Shut, the row is the one line a buyer scans a list by — how many tickets,
 * for how much, bought when — and the Reversed or Refund-in-progress badge,
 * which must be readable without opening anything: a reversed purchase that
 * looks like any other until clicked is the list lying. Open, it is
 * everything the old full card showed: the Ticket Sale Lines, the Sale
 * Confirmation reference a buyer quotes to a promoter, the Tax ID, the undo
 * offer or the state of the refund, and the Holder List with its questions.
 *
 * A native `<details>`, not a client component: the page is a server render,
 * the state is the browser's own, and a row opens before any script runs.
 */
export async function TicketSaleCard({
  sale,
  defaultOpen = false,
  badgeReversed = true,
  viaConfirmationLink = false,
  customerEmail = null,
}: {
  sale: TicketSale;
  /**
   * Whether a reversed Sale wears its badge on the row. True everywhere but
   * the Reversed tab, whose heading and description already say it of every
   * row there: a badge repeated down a list that is nothing else stops being
   * a warning and becomes wallpaper.
   */
  badgeReversed?: boolean;
  /**
   * True when the row should start open — the Event has only this one Sale,
   * so there is nothing to choose between and nothing to hide.
   */
  defaultOpen?: boolean;
  /**
   * True when this card is being drawn for someone who arrived by a Confirmation
   * Link rather than by signing in.
   *
   * That page stays strictly read-only (#121). A link travels by email and gets
   * forwarded, quoted in support threads and pasted into chats, and reversal is
   * the one destructive, money-moving action a Customer has — which is precisely
   * why #119 put it behind a Customer Session and not behind the link. So this
   * card offers the deadline and the way to sign in, and never the button.
   *
   * The API refuses the undo behind a link session anyway; drawing a button that
   * would come back refused is not a safety measure, it is a worse version of
   * telling somebody the truth.
   */
  viaConfirmationLink?: boolean;
  /** The address this session belongs to, to prefill sign-in with. */
  customerEmail?: string | null;
}) {
  const t = await getTranslations("customerArea");
  const formatLocale = await getFormatLocale();
  // The day this was bought, in the Customer's language; null only for a
  // sold_at the API never sends, in which case the row simply omits it.
  const boughtOn = formatPurchaseDate(sale.sold_at, formatLocale);
  const reversed = sale.status === "reversed";
  // A Reversal Request in flight: the Customer asked to undo this, the Payment
  // Provider has not said what it did, and the platform is finding out (ADR
  // 0024). Everything below it renders exactly as it does for any other active
  // sale — the lines, the total, the tickets — because that is what it is. No
  // money is known to have moved, the capacity is still held, and these tickets
  // are still valid for entry; dimming them would be this card inventing an
  // outcome the API pointedly declined to state.
  //
  // Guarded on `reversed` only so the two states can never be drawn at once: a
  // resolved request leaves the sale reversed and the pending flag false, and if
  // a read ever showed both, "Reversed" is the one that has actually happened.
  const refundPending = sale.reversal_pending && !reversed;
  const totalTickets = sale.lines.reduce((sum, line) => sum + line.quantity, 0);
  // The Tax ID this sale was transacted under, so a Customer can tell a personal
  // purchase from one made under a company RUC (#99). Sales recorded before the
  // feature — and imported ones that never carried an ID — show a plain "—";
  // history is never backfilled, so an honest blank is the whole rendering.
  const taxId = formatTaxId(sale.tax_id_type, sale.tax_id_number);
  // The undo offer (#119). The API answers `reversible` and the closing instant
  // together and sends null for the second whenever the first is false, so a
  // sale that cannot be undone has no deadline to draw and this card says
  // nothing at all about undoing it — no greyed-out button, no expired
  // countdown, nothing to explain.
  //
  // The card trusts that answer rather than recomputing it, and reads it through
  // the same helper the guest surfaces use (#121), so one sale cannot show one
  // deadline here and another on the page a buyer saw before they signed in.
  //
  // The deadline's words follow the page's language; its clock stays Ecuador's,
  // which is the Reversal Window's own (ADR 0018).
  const reversalDeadline = undoDeadline(sale, formatLocale);

  return (
    // The row is addressable (#121). The Customer Area is one page listing every
    // purchase and has no per-sale route, so this anchor is what lets the guest
    // surfaces — and the sign-in they lead through — land a buyer on the sale
    // they came about rather than on a list to scan. The id spelling lives in
    // lib/destination beside the links that use it, so the two cannot drift.
    // Arriving by the anchor also OPENS the row: components/open-sale-from-hash.
    //
    // `open` is the attribute or nothing, never `false`: React re-applies a
    // `false` on every redraw, and a save inside the row (a Holder's answer)
    // would snap it shut on the way back.
    <details
      id={ticketSaleAnchorId(sale.id)}
      open={defaultOpen || undefined}
      // A NAMED group, because the Holder List rows nested inside are
      // `<details>` with chevrons of their own: Tailwind's bare `group-open:`
      // matches any open `.group` ancestor, so an open Sale row would turn
      // every chevron inside it without opening anything.
      className="group/sale scroll-mt-6"
    >
      {/* THE WHOLE ROW IS THE TAP TARGET, at least 44px tall; the chevron is a
          hint, not the handle. The facts wrap rather than truncate: at 360px
          "3 tickets · $45.00" and "Bought Jul 5, 2026" take a line each and
          the badge a third, and the page is never wider than the phone (#353). */}
      <summary className="flex min-h-11 cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm [&::-webkit-details-marker]:hidden">
        <span
          aria-hidden
          className="text-muted-foreground transition-transform group-open/sale:rotate-90"
        >
          ▸
        </span>
        <span className="font-medium">
          {/* One message, so the count's plural and the total it sits beside
              cannot be assembled in an order English happens to like. */}
          {t("ticketsTotal", {
            count: totalTickets,
            total: formatPrice(sale.amount_cents, sale.currency, formatLocale),
          })}
        </span>
        {boughtOn ? (
          <span className="text-muted-foreground">{t("boughtOn", { date: boughtOn })}</span>
        ) : null}
        {/* A reversed sale must say so plainly rather than sit in the list
            looking like tickets the Customer still holds — and on the shut
            row, where the list is read. */}
        {reversed && badgeReversed ? (
          <Badge variant="destructive">{t("reversedBadge")}</Badge>
        ) : null}
        {/* Not destructive, and deliberately: a destructive badge beside
            tickets that are still good would say the purchase is gone. This
            is work in progress on the money and nothing else. */}
        {refundPending ? <Badge variant="warning">{t("refundPendingBadge")}</Badge> : null}
      </summary>

      <div className="pb-4">
      <ul className="space-y-1 text-sm">
        {sale.lines.map((line, index) => (
          <li key={`${line.ticket_type_name}-${index}`} className="flex justify-between gap-4">
            <span>
              {t("lineQuantity", { count: line.quantity, ticketType: line.ticket_type_name })}
            </span>
            <span className="text-muted-foreground">
              {formatPrice(line.unit_price_cents * line.quantity, sale.currency, formatLocale)}
            </span>
          </li>
        ))}
      </ul>

      <div className="mt-4 space-y-1 border-t pt-4 text-sm text-muted-foreground">
          {/* The value is emphasised inside the sentence rather than after it,
              so a language that leads with the reference still can. */}
          <p>
            {t.rich("confirmation", {
              reference: sale.confirmation_ref,
              value: (chunks) => (
                <span className="font-mono font-medium text-foreground">{chunks}</span>
              ),
            })}
          </p>
          <p>
            {t.rich("taxId", {
              // "Cédula", "RUC" and "Pasaporte" name Ecuadorian documents and
              // read the same in both languages, so the formatted value is not
              // in the catalog. The em dash is punctuation, not copy.
              taxId: taxId ?? "—",
              value: (chunks) => <span className="font-medium text-foreground">{chunks}</span>,
            })}
          </p>
      </div>

      {/* The sentence, its Ecuador-time qualifier and the word "undo" all live in
          UndoWindowNotice, shared with the guest surfaces. What differs is the
          action beside it: a signed-in Customer gets the button that undoes, and
          somebody who arrived by a forwarded Confirmation Link gets the way to
          prove the address is theirs. What a paid purchase's undo does to the
          money is said in the dialog, where the Customer is deciding. */}
      {/* While a Reversal Request is in flight the deadline sentence and the
          button give way to the state of the refund. The Undo action is
          withdrawn rather than disabled: a second press is answered with the
          same pending request — no second provider call, no second row — so a
          button here would be one that visibly does nothing. The sentence that
          replaces it is what stops that withdrawal reading as the offer having
          silently expired.

          Two sentences for the one state, because only one of these surfaces can
          make the refund move. Loading a signed-in Customer Area is what asks the
          Payment Provider again about this Customer's own in-flight request (ADR
          0024), so there "check back in a few minutes" describes the mechanism. A
          Confirmation Link session drives no such thing — it is a read credential
          for one sale — so on that page the pending state sits unchanged however
          often the reader reloads, and inviting them back would be this card
          promising what the surface it is drawn on cannot deliver. It says what
          is true and stops. */}
      {refundPending ? (
        <p className="mt-4 border-t pt-4 text-sm text-muted-foreground">
          {viaConfirmationLink ? t("refundPendingNoteLink") : t("refundPendingNote")}
        </p>
      ) : reversalDeadline ? (
        <UndoWindowNotice deadline={reversalDeadline}>
          {viaConfirmationLink ? (
            <SignInToUndo signedIn={false} email={customerEmail} ticketSaleId={sale.id} />
          ) : (
            <UndoPurchase
              saleId={sale.id}
              eventName={sale.event.name}
              confirmationRef={sale.confirmation_ref}
              paidLabel={
                sale.amount_cents > 0
                  ? formatPrice(sale.amount_cents, sale.currency, formatLocale)
                  : null
              }
            />
          )}
        </UndoWindowNotice>
      ) : null}

      {/* And on the surface that can make it move, the Customer need not reload
          to find out: the page re-asks itself while the answer is outstanding
          (#160). It is left off the Confirmation Link surface by the same split
          that decides the sentence above — the backend drains an in-flight
          Reversal Request only for a full Customer Session, so a timer there
          would be re-reading a state its own reads can never change.

          It draws nothing, so it sits last rather than between the sentences
          above and the comment that explains them. */}
      {viaConfirmationLink ? null : (
        <ReversalWatch
          pending={refundPending}
          reversed={reversed}
          reversalStatus={sale.reversal_status}
        />
      )}

      {/* The Tickets on this Sale, their Ticket Questions and the per-Ticket
          Answer Links the buyer passes on (#315, ADR 0044).

          IT IS DRAWN ON BOTH SURFACES, unlike the undo above it, and the
          difference between the two is the point. The undo moves money and
          cannot be taken back, so it demands a full Customer Session and a
          forwarded receipt does not get one. Answering a Ticket Question sets
          somebody's t-shirt size, is correctable by the buyer, the holder and
          Event Staff alike, and is the thing a person opening their receipt is
          most likely there to do — ADR 0044 already lets an unauthenticated
          stranger do it through an Answer Link, so refusing the buyer holding
          their own receipt would be a stricter rule for the owner than for the
          public.

          It draws NOTHING at all unless this sale's Ticket Types ask something,
          which most do not, and nothing while the feature is dark. So the great
          majority of these cards are exactly the card they were before — once
          the lists have landed. Until then it holds one row per Ticket, and
          the count is this card's to give: the lines know it (#357). */}
      <BuyerTicketAnswers ticketSaleId={sale.id} ticketCount={totalTickets} />
      </div>
    </details>
  );
}
