import Link from "next/link";

import { Badge } from "@ticket-pos/ui";

import { UndoPurchase } from "@/components/undo-purchase";
import { SignInToUndo, UndoWindowNotice } from "@/components/undo-window-notice";
import type { TicketSale } from "@/lib/customer-session";
import { formatEventDateTime, formatPrice } from "@/lib/format";
import { formatTaxId } from "@/lib/tax-id";
import { undoDeadline } from "@/lib/undo-window";

/**
 * One entry in the Customer Area: a Ticket Sale shown by the thing the Customer
 * actually cares about — the Event — with the Organization behind it, what was
 * bought, and the Sale Confirmation reference they can quote to a promoter.
 */
export function TicketSaleCard({
  sale,
  viaConfirmationLink = false,
  customerEmail = null,
}: {
  sale: TicketSale;
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
  const dateLabel = formatEventDateTime(sale.event.starts_at, sale.event.timezone);
  const reversed = sale.status === "reversed";
  const eventHref = `/${sale.organization.slug}/events/${sale.event.slug}`;
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
  const reversalDeadline = undoDeadline(sale);

  return (
    <li className="rounded-lg border bg-card p-5 sm:p-6">
      <div className="flex flex-col gap-1">
        <h3 className="text-lg font-semibold tracking-tight">
          <Link href={eventHref} className="hover:text-primary hover:underline">
            {sale.event.name}
          </Link>
        </h3>
        <p className="text-sm text-muted-foreground">
          {dateLabel ?? "Date to be announced"}
          {sale.event.venue_name ? ` · ${sale.event.venue_name}` : ""}
        </p>
        <p className="text-sm text-muted-foreground">
          Presented by{" "}
          <Link href={`/${sale.organization.slug}`} className="hover:text-foreground hover:underline">
            {sale.organization.name}
          </Link>
        </p>
      </div>

      <ul className="mt-4 space-y-1 text-sm">
        {sale.lines.map((line, index) => (
          <li key={`${line.ticket_type_name}-${index}`} className="flex justify-between gap-4">
            <span>
              {line.quantity} × {line.ticket_type_name}
            </span>
            <span className="text-muted-foreground">
              {formatPrice(line.unit_price_cents * line.quantity, sale.currency)}
            </span>
          </li>
        ))}
      </ul>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t pt-4 text-sm">
        <div className="space-y-1 text-muted-foreground">
          <p>
            Confirmation{" "}
            <span className="font-mono font-medium text-foreground">{sale.confirmation_ref}</span>
          </p>
          <p>
            Tax ID{" "}
            <span className="font-medium text-foreground">{taxId ?? "—"}</span>
          </p>
        </div>
        <div className="flex items-center gap-3">
          {/* A reversed sale must say so plainly rather than sit in the list
              looking like tickets the Customer still holds. */}
          {reversed ? <Badge variant="destructive">Reversed</Badge> : null}
          <p className="font-medium">
            {totalTickets} {totalTickets === 1 ? "ticket" : "tickets"} ·{" "}
            {formatPrice(sale.amount_cents, sale.currency)}
          </p>
        </div>
      </div>

      {/* The sentence, its Ecuador-time qualifier and the word "undo" all live in
          UndoWindowNotice, shared with the guest surfaces. What differs is the
          action beside it: a signed-in Customer gets the button that undoes, and
          somebody who arrived by a forwarded Confirmation Link gets the way to
          prove the address is theirs. What a paid purchase's undo does to the
          money is said in the dialog, where the Customer is deciding. */}
      {reversalDeadline ? (
        <UndoWindowNotice deadline={reversalDeadline}>
          {viaConfirmationLink ? (
            <SignInToUndo signedIn={false} email={customerEmail} />
          ) : (
            <UndoPurchase
              saleId={sale.id}
              eventName={sale.event.name}
              confirmationRef={sale.confirmation_ref}
              paidLabel={
                sale.amount_cents > 0 ? formatPrice(sale.amount_cents, sale.currency) : null
              }
            />
          )}
        </UndoWindowNotice>
      ) : null}
    </li>
  );
}
