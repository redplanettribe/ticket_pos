import { fetchEventsJSON } from "./events-api";

/**
 * An Affiliate Link: a named, trackable link to an Event's page. The code is
 * system-generated and immutable, and `url` is the whole thing an organizer
 * copies — the API derives it from the Storefront origin it is configured with,
 * so this app never assembles a Storefront URL itself.
 *
 * Later tickets may add fields to the same rows; read fields by name rather
 * than assuming this is the full set.
 */
export type AffiliateLink = {
  id: string;
  name: string;
  code: string;
  active: boolean;
  url: string;
  /** Raw visits to the Event page through this link — repeat visits included. */
  clicks: number;
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

/**
 * Renames an Affiliate Link and/or takes it out of circulation. Both fields are
 * optional and an omitted one is left alone: the code and its URL never change
 * either way, so a deactivated link resumes under exactly the same link when it
 * is reactivated.
 */
export async function updateAffiliateLink(
  eventId: string,
  linkId: string,
  patch: { name?: string; active?: boolean },
): Promise<AffiliateLink> {
  return fetchEventsJSON<AffiliateLink>(`/api/events/${eventId}/affiliate-links/${linkId}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

/**
 * Deletes an Affiliate Link. The API refuses one that has any clicks or any
 * attributed sale (a reversed one included) — history is never rewritten, and
 * the way to retire a link that worked is to deactivate it.
 */
export async function deleteAffiliateLink(eventId: string, linkId: string): Promise<void> {
  await fetchEventsJSON<{ message: string }>(`/api/events/${eventId}/affiliate-links/${linkId}`, {
    method: "DELETE",
  });
}
