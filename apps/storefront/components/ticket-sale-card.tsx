import Link from "next/link";

import { Badge } from "@ticket-pos/ui";

import type { TicketSale } from "@/lib/customer-session";
import { formatEventDateTime, formatPrice, formatReversalDeadline } from "@/lib/format";
import { formatTaxId } from "@/lib/tax-id";

/**
 * One entry in the Customer Area: a Ticket Sale shown by the thing the Customer
 * actually cares about — the Event — with the Organization behind it, what was
 * bought, and the Sale Confirmation reference they can quote to a promoter.
 */
export function TicketSaleCard({ sale }: { sale: TicketSale }) {
  const dateLabel = formatEventDateTime(sale.event.starts_at, sale.event.timezone);
  const reversed = sale.status === "reversed";
  const eventHref = `/${sale.organization.slug}/events/${sale.event.slug}`;
  const totalTickets = sale.lines.reduce((sum, line) => sum + line.quantity, 0);
  // The Tax ID this sale was transacted under, so a Customer can tell a personal
  // purchase from one made under a company RUC (#99). Sales recorded before the
  // feature — and imported ones that never carried an ID — show a plain "—";
  // history is never backfilled, so an honest blank is the whole rendering.
  const taxId = formatTaxId(sale.tax_id_type, sale.tax_id_number);
  // The Reversal Window, when this sale is inside one (#118). The API answers
  // `reversible` and the closing instant together and sends null for the second
  // whenever the first is false, so a sale that cannot be undone has no deadline
  // to draw and this card says nothing at all about reversing. There is no undo
  // button yet — the Customer is told how long they have, and that is the whole
  // of this release.
  const reversalDeadline =
    sale.reversible && sale.reversal_window_closes_at
      ? formatReversalDeadline(sale.reversal_window_closes_at)
      : null;

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

      {/* The deadline is named in Ecuador time and says so: it is the platform's
          own wall clock, not the Event's, and a buyer whose show is abroad would
          otherwise have no way to tell which 8:00 PM was meant. */}
      {reversalDeadline ? (
        <p className="mt-4 border-t pt-4 text-sm text-muted-foreground">
          You can cancel this purchase until{" "}
          <span className="font-medium text-foreground">{reversalDeadline}</span> Ecuador time.
        </p>
      ) : null}
    </li>
  );
}
