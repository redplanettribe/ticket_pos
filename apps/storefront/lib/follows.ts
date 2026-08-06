import type { Follow, FollowedTag, Follows, SessionOutcome } from "./customer-session";

/**
 * The Follows, as the pages that draw Follow controls need them (#218).
 *
 * Everything here is a pure function of one listing, and that is the point:
 * every page with a Follow control on it asks the same two questions — where do
 * I write, and does the Customer already follow this — and neither answer should
 * be re-derived per surface. The Event page, the explorer and the Following list
 * all read one Follows list and consult these.
 *
 * They are also the only part of this feature in the Storefront that can be
 * tested here at all: `node --test` runs `lib/*.test.ts` and this app has no
 * component renderer, so the discriminated-union narrowing that every surface
 * depends on is pulled into functions rather than left inline in JSX where
 * nothing could reach it.
 */

/** Where the Follow control for an Organization writes. */
export function organizationFollowEndpoint(slug: string): string {
  return `/api/customer/follows/organizations/${encodeURIComponent(slug)}`;
}

/**
 * Where the Follow control for a Tag writes.
 *
 * The canonical key is encoded because it is not URL-safe: several Preset Tags
 * carry a space and one an ampersand, and an unencoded "arts & theatre" would
 * arrive at the route as "arts " with a stray query string.
 */
export function tagFollowEndpoint(canonicalKey: string): string {
  return `/api/customer/follows/tags/${encodeURIComponent(canonicalKey)}`;
}

/**
 * Whether the Customer Follows this Organization, given the one listing the page
 * already read.
 *
 * It takes the SessionOutcome rather than the data, so a signed-out or failed
 * read answers "no" instead of forcing every caller to write the same guard. A
 * page still checks the outcome itself to decide whether to draw a control at
 * all — "not following" and "cannot say" are the same answer here and very
 * different decisions there.
 */
export function followsOrganization(
  follows: SessionOutcome<Follows>,
  slug: string,
): boolean {
  return followList(follows).some(
    (follow) => follow.type === "organization" && follow.organization.slug === slug,
  );
}

/** Whether the Customer Follows this Tag, by canonical key. */
export function followsTag(follows: SessionOutcome<Follows>, canonicalKey: string): boolean {
  return followList(follows).some(
    (follow) => follow.type === "tag" && follow.tag.canonical_key === canonicalKey,
  );
}

/**
 * The entries, or none when the read did not produce any.
 *
 * The API returns the two kinds already interleaved in one order, so nothing
 * here sorts, groups or splits them: a Following list renders this top to
 * bottom. Re-ordering in the browser is how two surfaces come to disagree about
 * what "most recent" means.
 */
export function followList(follows: SessionOutcome<Follows>): Follow[] {
  return follows.status === "ok" ? follows.data.follows : [];
}

/**
 * The Tag the Customer Follows under this canonical key, or null.
 *
 * This exists for the Suggested Follows panel's reason line (#232). A suggestion
 * names the Tag that produced it BY CANONICAL KEY and by nothing else — no
 * display name and no language crosses the wire (ADR 0027) — and the key alone
 * cannot be worded: the `tags` message catalogue holds copy for Preset Tags
 * only, so a Custom Tag's key would render as the lowercased string somebody
 * typed.
 *
 * The listing the same page already read is where the missing half is. Every
 * reason names a Tag the Customer Follows DIRECTLY, so the subject is always in
 * that list, carrying the `name` and the `curated` flag `tagName()` needs to
 * decide between this app's own copy and the Organization's own word.
 *
 * Null rather than a guess when it is not there, which happens when the listing
 * read failed while the suggestions read succeeded — the two are independent
 * (ADR 0031). The panel drops the reason line in that case rather than printing
 * a raw key at a reader.
 */
export function followedTag(
  follows: SessionOutcome<Follows>,
  canonicalKey: string,
): FollowedTag | null {
  for (const follow of followList(follows)) {
    if (follow.type === "tag" && follow.tag.canonical_key === canonicalKey) return follow.tag;
  }
  return null;
}
