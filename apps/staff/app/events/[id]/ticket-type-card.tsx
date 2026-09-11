"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useTranslations } from "next-intl";

import { Badge, Button, cn } from "@ticket-pos/ui";

import type { TicketType } from "@/lib/events-api";
import { formatDateTime, formatMoney, formatNumber } from "@/lib/format";
import { promotionState, promotionStateBadgeVariant } from "@/lib/promotions";
import { purchaseLimitLine } from "@/lib/purchase-limit";
import {
  availabilityBadgeVariant,
  capacityCounts,
  soldShare,
  ticketTypeAvailability,
  type Availability,
} from "@/lib/ticket-type-availability";
import type { PromotionState } from "@/lib/promotions";

type TicketTypeCardProps = {
  ticketType: TicketType;
  eventStatus: string;
  /**
   * The Event's timezone: Promotion windows are read in it (ADR 0021), and so
   * is the Sales Cutoff (ADR 0070).
   */
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
  /**
   * Opens the Ticket Question authoring surface, or undefined where the feature
   * flag is off — in which case the card draws no button for it at all (#309,
   * ADR 0045). Undefined rather than a boolean beside a handler so that "there
   * is nowhere to go" and "there is a button" cannot disagree.
   */
  onQuestions?: () => void;
  onDelete: () => void;
};

/** The `ticketTypes` catalog key an availability state is said with. */
const AVAILABILITY_KEYS = {
  not_on_sale: "availabilityNotOnSale",
  sold_out: "availabilitySoldOut",
  closed: "availabilityClosed",
  low_stock: "availabilityLowStock",
  on_sale: "availabilityOnSale",
} as const satisfies Record<Availability, string>;

/**
 * The capacity meter's fill per availability state.
 *
 * A record rather than the conditional cascade this used to be, so that a state
 * added to the union is a compile error here instead of quietly inheriting the
 * confident primary fill — which is exactly what closed did on its first pass.
 * Closed draws the same spent fill sold out does, because neither is selling
 * and the meter is not what tells the two apart: the badge above carries the
 * word, which is the whole reason colour is never the only signal
 * (docs/design/foundation.md, ADR 0070).
 */
const AVAILABILITY_METER_FILL = {
  // A draft Event's stock is untouched rather than spent, and reads as it
  // always has.
  not_on_sale: "bg-primary",
  sold_out: "bg-muted-foreground",
  closed: "bg-muted-foreground",
  low_stock: "bg-warning",
  on_sale: "bg-primary",
} as const satisfies Record<Availability, string>;

/** The same for a Promotion's state, badge prefix included. */
const PROMOTION_STATE_KEYS = {
  scheduled: "promotionScheduled",
  live: "promotionLive",
  ended: "promotionEnded",
} as const satisfies Record<PromotionState, string>;

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
  onQuestions,
  onDelete,
}: TicketTypeCardProps) {
  const t = useTranslations("ticketTypes");
  const locale = toAppLocale(useLocale());
  // One clock read for the whole card. Two separate `new Date()` calls can
  // straddle a second, and this card judges two windows against the same
  // moment: a render must not be able to say a Promotion ended before a cutoff
  // it shares an instant with.
  const now = new Date();
  const availability = ticketTypeAvailability(ticketType, eventStatus, now);
  const promotion = ticketType.promotion;
  const state = promotion ? promotionState(promotion, now) : null;
  // The Promotional Price only displaces the List Price while its window holds;
  // scheduled and ended ones read as a note under the price that still applies.
  const promotionLive = state === "live";
  const share = soldShare(ticketType);
  // A Purchase Limit is unset on most Ticket Types, so the line appears only
  // when one is actually set: null here means the card stays silent rather than
  // claiming anything about how many a Customer may hold.
  const purchaseLimit = purchaseLimitLine(ticketType.max_per_customer);
  const capacity = capacityCounts(ticketType);

  // The Ticket Type's own currency and the Event's own timezone, whichever
  // language is being read: a Locale decides marks and never money or time
  // (ADR 0041).
  const listPrice = formatMoney(ticketType.price_cents, ticketType.currency, locale);
  const promotionPrice = promotion
    ? formatMoney(promotion.promotional_price_cents, ticketType.currency, locale)
    : null;
  // The Sales Cutoff as configured, on the Event's clock — not a verdict about
  // whether it has passed, which the badge above already carries. Stated in
  // every state and only when one is actually set, because nothing validates
  // the value and this line is the only place a typo in the year can show
  // itself (ADR 0070).
  const salesCutoff = formatDateTime(ticketType.sales_cutoff_at, timezone, locale);
  const promotionEnds = promotion ? formatDateTime(promotion.ends_at, timezone, locale) : null;
  const promotionStarts =
    promotion && promotion.starts_at
      ? formatDateTime(promotion.starts_at, timezone, locale)
      : null;
  // Two messages rather than one glued together from a conditional fragment:
  // "from X until Y" and "until Y" are different sentences, and in Spanish they
  // are not the same one with a piece missing.
  const promotionWindow = !promotionEnds
    ? null
    : promotionStarts
      ? t("promotionWindow", { start: promotionStarts, end: promotionEnds })
      : t("promotionWindowOpenEnded", { end: promotionEnds });

  return (
    <li className="rounded-md border p-4 transition-colors hover:bg-muted/40">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            {/* The Ticket Type's name and description are the Organization's
                own words: data, never copy, and untranslated in both. */}
            <h3 className="font-medium">{ticketType.name}</h3>
            <Badge variant={availabilityBadgeVariant(availability)}>
              {t(AVAILABILITY_KEYS[availability])}
            </Badge>
            {state ? (
              <Badge variant={promotionStateBadgeVariant(state)}>
                {t(PROMOTION_STATE_KEYS[state])}
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
            <span className="sr-only">
              {promotionLive ? t("promotionalPriceSr") : t("priceSr")}{" "}
            </span>
            {promotionLive ? promotionPrice : listPrice}
          </p>
          {promotionLive ? (
            <p className="text-sm text-muted-foreground">
              <span className="sr-only">{t("listPriceSr")} </span>
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
              AVAILABILITY_METER_FILL[availability],
            )}
            style={{ width: `${share * 100}%` }}
          />
        </div>
        {/* The counts come from the pure module and the sentence from the
            catalog, with each number drawn in the Staff Locale rather than the
            browser's (ADR 0041). */}
        <p className="text-sm text-muted-foreground tabular-nums">
          {t("capacityLine", {
            sold: formatNumber(capacity.sold, locale),
            capacity: formatNumber(capacity.capacity, locale),
            remaining: formatNumber(capacity.remaining, locale),
          })}
        </p>
        {purchaseLimit ? (
          <p className="text-sm text-muted-foreground tabular-nums">
            {t("purchaseLimitLine", { limit: formatNumber(purchaseLimit.limit, locale) })}
          </p>
        ) : null}
        {/* A line of its own, whatever state is winning above: sold out, closed
            or on sale, the configured cutoff still reads back. Unset on most
            Ticket Types, and then the card says nothing rather than claiming
            anything about when this one stops selling. */}
        {salesCutoff ? (
          <p className="text-sm text-muted-foreground tabular-nums">
            {t("salesCutoffLine", { datetime: salesCutoff })}
          </p>
        ) : null}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
        <div className="mr-auto flex items-center gap-1 sm:mr-0">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isFirst}
            aria-label={t("moveUp", { name: ticketType.name })}
            onClick={onMoveUp}
          >
            <span aria-hidden>↑</span>
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isLast}
            aria-label={t("moveDown", { name: ticketType.name })}
            onClick={onMoveDown}
          >
            <span aria-hidden>↓</span>
          </Button>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onEdit}>
          {t("edit")}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onPromotion}>
          {promotion === null ? t("addPromotion") : t("promotion")}
        </Button>
        {onQuestions ? (
          <Button type="button" variant="ghost" size="sm" onClick={onQuestions}>
            {t("questions")}
          </Button>
        ) : null}
        {canDelete ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
            onClick={onDelete}
          >
            {t("delete")}
          </Button>
        ) : null}
      </div>
    </li>
  );
}
