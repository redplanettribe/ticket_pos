/**
 * Counting a visit to an Event page that was reached through an Affiliate Link.
 *
 * The whole feature on this side is one line in the page: read the ref, report
 * it, render as if neither had happened. Nothing here may change what a buyer
 * sees — a dead code, an unknown one, a mistyped one, and an API that is down
 * all look identical from the page, because the counter is display-only stats
 * for an organizer and never a step in buying a ticket.
 */

// Extensioned so the node:test runner, which loads this module for the pure
// helper below, can resolve it — the same reason the other tested helpers do.
import { callBackend } from "./api.ts";

/**
 * The longest a ref may be and still be worth reporting. Generated codes are
 * eight characters; this leaves room for a format change and still refuses the
 * arbitrary junk anyone can put in a query string, before it becomes a request.
 */
const MAX_REF_LENGTH = 64;

/**
 * affiliateCodeFromRef reads the Affiliate Link code out of an Event page's
 * `?ref=` value, or null when there is nothing worth reporting.
 *
 * It never decides whether the code is live — dead codes are the endpoint's
 * business, and asking first would make the buyer's page wait on the answer.
 */
export function affiliateCodeFromRef(ref: string | string[] | undefined): string | null {
  // A repeated ref arrives as an array; the first one is the link that was
  // followed.
  const raw = Array.isArray(ref) ? ref[0] : ref;
  if (typeof raw !== "string") return null;
  const code = raw.trim();
  if (code === "" || code.length > MAX_REF_LENGTH) return null;
  return code;
}

/**
 * recordAffiliateClick reports one visit to an Event page reached through an
 * Affiliate Link, fire-and-forget.
 *
 * Call it without awaiting: the returned promise resolves whatever happens, so
 * a caller that does await it still cannot be made to fail, and a caller that
 * does not cannot produce an unhandled rejection. Every failure — a dead code
 * (accepted and ignored by the API), a network error, an API outage — is
 * swallowed on purpose.
 */
export async function recordAffiliateClick(
  orgSlug: string,
  eventSlug: string,
  code: string,
): Promise<void> {
  try {
    await callBackend(
      `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(
        eventSlug,
      )}/affiliate-links/${encodeURIComponent(code)}/click`,
      { method: "POST" },
    );
  } catch {
    // Display-only stats. A click that goes uncounted costs an organizer one
    // number; a click that throws would cost a buyer their page.
  }
}
