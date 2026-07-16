type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

function apiBaseUrl(): string {
  const url = process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return url.replace(/\/$/, "");
}

export type PublicOrganization = {
  name: string;
  slug: string;
  logo_url: string | null;
};

export type PublicOrganizationSummary = {
  name: string;
  slug: string;
  logo_url: string | null;
};

export type PublicEventCard = {
  slug: string;
  name: string;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string | null;
  venue_name: string | null;
  cover_image_url: string | null;
  organization: PublicOrganizationSummary;
  currency: string;
  price_from_cents: number | null;
  sold_out: boolean;
};

export type PublicTicketType = {
  name: string;
  description: string | null;
  price_cents: number;
  currency: string;
  remaining: number;
  sold_out: boolean;
};

export type PublicEventDetail = {
  slug: string;
  name: string;
  description: string | null;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string | null;
  venue_name: string | null;
  venue_address: string | null;
  cover_image_url: string | null;
  has_ended: boolean;
  organization: PublicOrganizationSummary;
  currency: string;
  ticket_types: PublicTicketType[];
};

export type PublicEventPage = {
  events: PublicEventCard[];
  next_cursor: string | null;
};

export type PublicOrganizationEvents = {
  organization: PublicOrganizationSummary;
  upcoming: PublicEventCard[];
  past: PublicEventCard[];
};

// Shared page size for the global explorer so the initial SSR page and each
// client "Load more" fetch request the same number of cards.
export const EXPLORER_PAGE_SIZE = 12;

export type ListEventsParams = {
  q?: string;
  from?: string;
  to?: string;
  cursor?: string;
  limit?: number;
};

async function fetchData<T>(path: string): Promise<T | null> {
  try {
    const response = await fetch(`${apiBaseUrl()}${path}`, { cache: "no-store" });
    const envelope = (await response.json()) as APIEnvelope<T>;
    if (!response.ok || envelope.error) {
      return null;
    }
    return envelope.data;
  } catch {
    return null;
  }
}

export async function getPublicOrganization(slug: string): Promise<PublicOrganization | null> {
  return fetchData<PublicOrganization>(`/api/v1/public/organizations/${encodeURIComponent(slug)}`);
}

export async function listPublicEvents(params: ListEventsParams = {}): Promise<PublicEventPage | null> {
  const query = new URLSearchParams();
  if (params.q) query.set("q", params.q);
  if (params.from) query.set("from", params.from);
  if (params.to) query.set("to", params.to);
  if (params.cursor) query.set("cursor", params.cursor);
  if (params.limit) query.set("limit", String(params.limit));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return fetchData<PublicEventPage>(`/api/v1/public/events${suffix}`);
}

export async function getOrganizationEvents(slug: string): Promise<PublicOrganizationEvents | null> {
  return fetchData<PublicOrganizationEvents>(
    `/api/v1/public/organizations/${encodeURIComponent(slug)}/events`,
  );
}

export async function getPublicEvent(
  orgSlug: string,
  eventSlug: string,
): Promise<PublicEventDetail | null> {
  return fetchData<PublicEventDetail>(
    `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(eventSlug)}`,
  );
}
