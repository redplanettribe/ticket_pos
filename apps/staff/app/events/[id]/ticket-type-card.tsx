"use client";

import { Badge, Button, cn } from "@ticket-pos/ui";

import { formatEventStartDate, formatPriceCents, type TicketType } from "@/lib/events-api";
import {
  PROMOTION_STATE_LABELS,
  promotionState,
  promotionStateBadgeVariant,
} from "@/lib/promotions";
import { purchaseLimitSummary } from "@/lib/purchase-limit";
import {
  AVAILABILITY_LABELS,
  availabilityBadgeVariant,
  capacitySummary,
  soldShare,
  ticketTypeAvailability,
} from "@/lib/ticket-type-availability";

type TicketTypeCardProps = {
  ticketType: TicketType;
  eventStatus: string;
  /** The Event's timezone: Promotion windows are read in it (ADR 0021). */
  timezone: string;
  /** What the price means for the buyer or the Organization under the fee handling (ADR 0014). */
  buyerPriceLine: string | null;
  isFirst: boolean;
  isLast: boolean;
  canDelete: boolean;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onEdit: () => void;
  onPromotion: () => void;
  onDelete: () => void;
};

/**
 * One Ticket Type as the staff editor shows it: what it is and whether it sells
 * (left), what it costs (right), how much of it is gone (under both), and what
 * can be done to it (bottom). Purely presentational — every mutation is the
 * section's.
 */
export function TicketTypeCard({
  ticketType,
  eventStatus,
  timezone,
  buyerPriceLine,
  isFirst,
  isLast,
  canDelete,
  onMoveUp,
  onMoveDown,
  onEdit,
  onPromotion,
  onDelete,
}: TicketTypeCardProps) {
  const availability = ticketTypeAvailability(ticketType, eventStatus);
  const promotion = ticketType.promotion;
  const state = promotion ? promotionState(promotion, new Date()) : null;
  // The Promotional Price only displaces the List Price while its window holds;
  // scheduled and ended ones read as a note under the price that still applies.
  const promotionLive = state === "live";
  const share = soldShare(ticketType);
  // A Purchase Limit is unset on most Ticket Types, so the line appears only
  // when one is actually set: null here means the card stays silent rather than
  // claiming anything about how many a Customer may hold.
  const purchaseLimitLine = purchaseLimitSummary(ticketType.max_per_customer);

  const listPrice = formatPriceCents(ticketType.price_cents, ticketType.currency);
  const promotionPrice = promotion
    ? formatPriceCents(promotion.promotional_price_cents, ticketType.currency)
    : null;
  const promotionWindow = promotion
    ? `${promotion.starts_at ? `from ${formatEventStartDate(promotion.starts_at, timezone)} ` : ""}until ${formatEventStartDate(promotion.ends_at, timezone)}`
    : null;

  return (
    <li className="rounded-md border p-4 transition-colors hover:bg-muted/40">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-medium">{ticketType.name}</h3>
            <Badge variant={availabilityBadgeVariant(availability)}>
              {AVAILABILITY_LABELS[availability]}
            </Badge>
            {state ? (
              <Badge variant={promotionStateBadgeVariant(state)}>
                Promo · {PROMOTION_STATE_LABELS[state]}
              </Badge>
            ) : null}
          </div>
          {/* A long description is the organizer's storefront copy, not
              something they scan the list for: two lines, so one verbose ticket
              type cannot push the rest of the list off the screen. */}
          {ticketType.description ? (
            <p className="line-clamp-2 text-sm text-muted-foreground">{ticketType.description}</p>
          ) : null}
        </div>

        <div className="shrink-0 tabular-nums sm:text-right">
          <p className="font-semibold">
            <span className="sr-only">{promotionLive ? "Promotional price " : "Price "}</span>
            {promotionLive ? promotionPrice : listPrice}
          </p>
          {promotionLive ? (
            <p className="text-sm text-muted-foreground">
              <span className="sr-only">List price </span>
              <span className="line-through">{listPrice}</span>
            </p>
          ) : null}
          {promotion && !promotionLive ? (
            <p className="text-sm text-muted-foreground">
              {promotionPrice} {promotionWindow}
            </p>
          ) : null}
          {promotionLive && promotionWindow ? (
            <p className="text-sm text-muted-foreground">{promotionWindow}</p>
          ) : null}
          {buyerPriceLine ? (
            <p className="text-sm text-muted-foreground">{buyerPriceLine}</p>
          ) : null}
        </div>
      </div>

      <div className="mt-3 space-y-1">
        <div aria-hidden className="h-1.5 overflow-hidden rounded-full bg-muted">
          <div
            className={cn(
              "h-full rounded-full transition-[width]",
              availability === "sold_out"
                ? "bg-muted-foreground"
                : availability === "low_stock"
                  ? "bg-warning"
                  : "bg-primary",
            )}
            style={{ width: `${share * 100}%` }}
          />
        </div>
        <p className="text-sm text-muted-foreground tabular-nums">{capacitySummary(ticketType)}</p>
        {purchaseLimitLine ? (
          <p className="text-sm text-muted-foreground tabular-nums">{purchaseLimitLine}</p>
        ) : null}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
        <div className="mr-auto flex items-center gap-1 sm:mr-0">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isFirst}
            aria-label={`Move ${ticketType.name} up`}
            onClick={onMoveUp}
          >
            <span aria-hidden>↑</span>
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isLast}
            aria-label={`Move ${ticketType.name} down`}
            onClick={onMoveDown}
          >
            <span aria-hidden>↓</span>
          </Button>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onEdit}>
          Edit
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onPromotion}>
          {promotion === null ? "Add promotion" : "Promotion"}
        </Button>
        {canDelete ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
            onClick={onDelete}
          >
            Delete
          </Button>
        ) : null}
      </div>
    </li>
  );
}
