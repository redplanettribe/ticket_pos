import type { CustomerArea, HeldTicket, TicketSale } from "@/lib/customer-session";

/**
 * The Customer Area's tabs and the Event groups inside them.
 *
 * THREE TABS, BY WHEN AND BY WHETHER THE PURCHASE STILL STANDS. "Upcoming"
 * and "Past" are the API's own split of the Customer's Ticket Sales, minus
 * every reversed one; "Reversed" is those, from either list, whatever their
 * date. A reversed Sale is never hidden — CONTEXT.md says it stays visible to
 * the Customer — but it is no longer a ticket to anything, so it sits with
 * the other reversed ones rather than among the Events the person is going
 * to. A Reversal Request still in flight is NOT a reversal (it may yet be
 * refused) and stays where the Sale was.
 *
 * ONE GROUP PER EVENT, NOT ONE CARD PER SALE. A person who bought three
 * times for the same DevFest thinks "my DevFest tickets", not "my three
 * DevFest purchases"; the Sales stay inside the group because the money, the
 * Sale Confirmation, the Tax ID and the Reversal Window are each a fact about
 * one Sale. A Ticket somebody ELSE bought and gave this Customer (a held
 * Ticket) joins the group of its Event for the same reason — it is a ticket
 * to that Event — while staying what it is: not a purchase, so no amount, no
 * reference and no undo are drawn for it.
 *
 * A held Ticket is filed under Upcoming or Past by the rule the API applies
 * to a Sale (backend/internal/customers/service/area.go, isUpcoming): past
 * once the Event has ended, or once it has started when no end is recorded;
 * an Event with no schedule has not happened yet. Restated here rather than
 * asked for because the held list carries no such flag, and two answers to
 * "is this Event over?" on one page would be the worse bug.
 */
export type CustomerAreaTab = "upcoming" | "past" | "reversed";

export const CUSTOMER_AREA_TABS: readonly CustomerAreaTab[] = ["upcoming", "past", "reversed"];

type EventSummary = TicketSale["event"];
type OrganizationSummary = TicketSale["organization"];

export type EventGroup = {
  event: EventSummary;
  organization: OrganizationSummary;
  /** This Customer's own Ticket Sales for the Event, as the API ordered them. */
  sales: TicketSale[];
  /** Tickets somebody else bought for the Event and this Customer accepted. */
  held: HeldTicket[];
};

/**
 * The tab named by the URL, or Upcoming when nothing (or nonsense) was named.
 * A bad value is a typo, not an error page.
 */
export function tabOf(raw: string | string[] | undefined): CustomerAreaTab {
  const value = Array.isArray(raw) ? raw[0] : raw;
  return CUSTOMER_AREA_TABS.find((tab) => tab === value) ?? "upcoming";
}

export function isUpcomingEvent(event: EventSummary, now: Date): boolean {
  if (event.ends_at !== null) return now.getTime() <= Date.parse(event.ends_at);
  if (event.starts_at !== null) return now.getTime() <= Date.parse(event.starts_at);
  return true;
}

export function isReversed(sale: TicketSale): boolean {
  return sale.status === "reversed";
}

/**
 * Which tabs to draw at all. Upcoming is always there — its empty state is
 * where the "discover events" offer lives — while Past and Reversed appear
 * only once there is something behind them: a tab labelled "Reversed" on the
 * page of somebody who has never undone anything is a question they did not
 * ask.
 */
export function availableTabs(area: CustomerArea, now: Date): CustomerAreaTab[] {
  return CUSTOMER_AREA_TABS.filter(
    (tab) => tab === "upcoming" || countFor(area, tab, now) > 0,
  );
}

/** How many tickets-to-something a tab holds: Sales plus held Tickets. */
export function countFor(area: CustomerArea, tab: CustomerAreaTab, now: Date): number {
  return groupsFor(area, tab, now).reduce(
    (sum, group) => sum + group.sales.length + group.held.length,
    0,
  );
}

/**
 * The Event groups for one tab, in the order the first Sale or held Ticket of
 * each Event appeared — the API orders upcoming soonest-first and past
 * latest-first, and this keeps that.
 */
export function groupsFor(area: CustomerArea, tab: CustomerAreaTab, now: Date): EventGroup[] {
  return groupSales(salesFor(area, tab), heldFor(area.holding ?? [], tab, now));
}

/**
 * The same grouping over a list already chosen — the page's Upcoming and Past
 * sections, until the tabs above take over (#355). It filters nothing: a
 * reversed Sale passed in stays in its Event's group, wearing its badge,
 * because until the Reversed tab exists there is nowhere else for it to be.
 */
export function groupSales(sales: TicketSale[], held: HeldTicket[] = []): EventGroup[] {
  const groups = new Map<string, EventGroup>();
  const groupFor = (event: EventSummary, organization: OrganizationSummary): EventGroup => {
    let group = groups.get(event.id);
    if (group === undefined) {
      group = { event, organization, sales: [], held: [] };
      groups.set(event.id, group);
    }
    return group;
  };
  for (const sale of sales) groupFor(sale.event, sale.organization).sales.push(sale);
  for (const ticket of held) groupFor(ticket.event, ticket.organization).held.push(ticket);
  return [...groups.values()];
}

function salesFor(area: CustomerArea, tab: CustomerAreaTab): TicketSale[] {
  switch (tab) {
    case "upcoming":
      return area.upcoming.filter((sale) => !isReversed(sale));
    case "past":
      return area.past.filter((sale) => !isReversed(sale));
    case "reversed":
      return [...area.upcoming, ...area.past].filter(isReversed);
  }
}

function heldFor(holding: HeldTicket[], tab: CustomerAreaTab, now: Date): HeldTicket[] {
  switch (tab) {
    case "upcoming":
      return holding.filter((ticket) => isUpcomingEvent(ticket.event, now));
    case "past":
      return holding.filter((ticket) => !isUpcomingEvent(ticket.event, now));
    case "reversed":
      // A Sale Reversal takes every Holder on the Sale with it: a held Ticket
      // on a reversed Sale is no longer held, and the API never lists it.
      return [];
  }
}

/**
 * The tab a Confirmation Link session should open on. The link names one
 * Sale; if that Sale has been reversed, opening on an empty Upcoming tab
 * would hide the one thing the reader came to see.
 */
export function tabForLinkedSale(area: CustomerArea): CustomerAreaTab {
  const sale = [...area.upcoming, ...area.past][0];
  if (sale === undefined) return "upcoming";
  if (isReversed(sale)) return "reversed";
  return area.upcoming.includes(sale) ? "upcoming" : "past";
}
