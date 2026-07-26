import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import type { EventDetail, TicketType } from "@/lib/events-api";
import { DEFAULT_SALES_STATUS, type SalesFilters } from "@/lib/sales-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { parseSaleDir, parseSaleSort } from "@/lib/sales-api";

import { loadSession } from "../../../staff-page-shell";
import { ImportSalesSection } from "../import-sales-section";
import { SalesList, type TicketTypeOption } from "../sales-list";
import { SalesRefreshProvider } from "../sales-refresh";
import { SalesSummaryStrip } from "../sales-summary-strip";

type SalesSearchParams = {
  page?: string;
  status?: string;
  ticket_type_id?: string;
  sold_from?: string;
  sold_to?: string;
  q?: string;
  channel?: string;
  source?: string;
  payment_method?: string;
  sort?: string;
  dir?: string;
};

type EventSalesPageProps = {
  params: Promise<{ id: string }>;
  searchParams: Promise<SalesSearchParams>;
};

// parsePage reads the URL page number, flooring at 1 (matches the API).
function parsePage(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "", 10);
  return Number.isFinite(parsed) && parsed >= 1 ? parsed : 1;
}

// parseFilters reads the filter values out of the URL (the source of truth so
// the view is shareable and survives a refresh); the backend re-validates them.
function parseFilters(searchParams: SalesSearchParams): SalesFilters {
  return {
    status: searchParams.status === "reversed" ? "reversed" : DEFAULT_SALES_STATUS,
    ticketTypeId: searchParams.ticket_type_id ?? "",
    soldFrom: searchParams.sold_from ?? "",
    soldTo: searchParams.sold_to ?? "",
    q: searchParams.q ?? "",
    channel: searchParams.channel ?? "",
    source: searchParams.source ?? "",
    paymentMethod: searchParams.payment_method ?? "",
  };
}

// fetchEventTimezone loads just the Event timezone (for sold-at formatting),
// tolerating failure so the list still renders in the viewer's local zone.
async function fetchEventTimezone(eventId: string, token: string): Promise<string | null> {
  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data?.timezone ?? null;
  } catch {
    return null;
  }
}

// fetchTicketTypeOptions loads the Event's Ticket Types for the ticket-type
// filter dropdown, tolerating failure so the list still renders (just without
// that filter's options).
async function fetchTicketTypeOptions(eventId: string, token: string): Promise<TicketTypeOption[]> {
  try {
    const envelope = await callBackend<TicketType[]>(`/api/v1/staff/events/${eventId}/ticket-types`, {
      method: "GET",
      sessionToken: token,
    });
    return (envelope.data ?? []).map((type) => ({ id: type.id, name: type.name }));
  } catch {
    return [];
  }
}

export default async function EventSalesPage({ params, searchParams }: EventSalesPageProps) {
  const { id } = await params;
  const resolvedSearchParams = await searchParams;
  const session = await loadSession();
  const role = session?.active_member?.role;

  // The Sales list is visible to every Member of the Event (Org Admin, Event
  // Owner, Event Staff). The Net Proceeds strip above it and the Sale Import tool
  // below it are for the owner: hired door staff see neither the Event's earnings
  // (the API refuses them the summary too) nor the import controls.
  const isOwner = role === "org_admin" || role === "event_owner";

  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  const [timezone, ticketTypes] = token
    ? await Promise.all([fetchEventTimezone(id, token), fetchTicketTypeOptions(id, token)])
    : [null, []];

  return (
    // SalesRefreshProvider lets the owner-only import section signal the Sales
    // list and the stat strip to re-read after a successful commit/undo.
    <SalesRefreshProvider>
      <div className="space-y-6">
        {isOwner ? <SalesSummaryStrip eventId={id} /> : null}
        <SalesList
          eventId={id}
          page={parsePage(resolvedSearchParams.page)}
          filters={parseFilters(resolvedSearchParams)}
          ticketTypes={ticketTypes}
          sort={parseSaleSort(resolvedSearchParams.sort)}
          dir={parseSaleDir(resolvedSearchParams.dir)}
          timezone={timezone}
        />
        {isOwner ? <ImportSalesSection eventId={id} /> : null}
      </div>
    </SalesRefreshProvider>
  );
}
