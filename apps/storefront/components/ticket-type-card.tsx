import { Badge, Card, CardContent } from "@ticket-pos/ui";

import type { PublicTicketType } from "@/lib/api";
import { formatPrice } from "@/lib/format";

function formatRemaining(remaining: number): string {
  return `${new Intl.NumberFormat("en-US").format(remaining)} remaining`;
}

export function TicketTypeCard({ ticketType }: { ticketType: PublicTicketType }) {
  return (
    <Card className={ticketType.sold_out ? "opacity-70" : undefined}>
      <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{ticketType.name}</h3>
            {ticketType.sold_out ? <Badge variant="secondary">Sold out</Badge> : null}
          </div>
          {ticketType.description ? (
            <p className="text-sm text-muted-foreground">{ticketType.description}</p>
          ) : null}
          {!ticketType.sold_out ? (
            <p className="text-sm text-muted-foreground">{formatRemaining(ticketType.remaining)}</p>
          ) : null}
        </div>
        <div className="shrink-0 text-right">
          <p className="font-semibold">{formatPrice(ticketType.price_cents, ticketType.currency)}</p>
        </div>
      </CardContent>
    </Card>
  );
}
