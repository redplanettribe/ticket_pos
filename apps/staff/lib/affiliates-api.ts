import { fetchEventsJSON } from "./events-api";

/**
 * An Affiliate Link: a named, trackable link to an Event's page. The code is
 * system-generated and immutable, and `url` is the whole thing an organizer
 * copies — the API derives it from the Storefront origin it is configured with,
 * so this app never assembles a Storefront URL itself.
 *
 * Later tickets add clicks to the same rows; read fields by name rather than
 * assuming this is the full set.
 */
export type AffiliateLink = {
  id: string;
  name: string;
  code: string;
  active: boolean;
  url: string;
  /**
   * The link's Affiliate Attribution figures: how many ACTIVE Ticket Sales it
   * drove and what they left the Organization after platform costs. Display-only
   * — nothing is owed on either — and a reversed sale drops out of both, however
   * it came to be reversed.
   */
  sales_count: number;
  net_proceeds_cents: number;
  created_at: string;
};

export const AFFILIATE_LINK_NAME_MAX_LENGTH = 80;

export async function listAffiliateLinks(eventId: string): Promise<AffiliateLink[]> {
  return fetchEventsJSON<AffiliateLink[]>(`/api/events/${eventId}/affiliate-links`);
}

export async function createAffiliateLink(eventId: string, name: string): Promise<AffiliateLink> {
  return fetchEventsJSON<AffiliateLink>(`/api/events/${eventId}/affiliate-links`, {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}
