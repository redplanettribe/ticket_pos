/**
 * Counting a load of an Event page, and the Affiliate Link it arrived through.
 *
 * The whole feature on this side is one line in the page: read the ref, report
 * the load, render as if neither had happened. Nothing here may change what a
 * buyer sees — a dead code, an unknown one, a mistyped one, and an API that is
 * down all look identical from the page, because the counters are display-only
 * stats for an organizer and never a step in buying a ticket.
 */

// Extensioned so the node:test runner, which loads this module for the pure
// helper below, can resolve it — the same reason the other tested helpers do.
import { normalizeAffiliateCode } from "./affiliate-code.ts";
import { callBackend } from "./api.ts";

/**
 * affiliateCodeFromRef reads the Affiliate Link code out of an Event page's
 * `?ref=` value, or null when there is nothing worth reporting.
 *
 * The rule is the attribution cookie's rule, imported rather than restated: the
 * click this counts and the click that cookie remembers are the same click, so
 * a ref the two disagreed about would credit a sale to a link whose counter
 * never moved.
 *
 * It never decides whether the code is live — dead codes are the endpoint's
 * business, and asking first would make the buyer's page wait on the answer.
 */
export function affiliateCodeFromRef(ref: string | string[] | undefined): string | null {
  return normalizeAffiliateCode(ref);
}

/**
 * recordEventPageView reports one load of an Event page, fire-and-forget,
 * carrying the Affiliate Link code the visitor arrived through when there was
 * one (ADR 0057). Every render reports — ref or no ref — because the whole
 * page's traffic is the baseline every link is compared against.
 *
 * Call it without awaiting: the returned promise resolves whatever happens, so
 * a caller that does await it still cannot be made to fail, and a caller that
 * does not cannot produce an unhandled rejection. Every failure — a dead code
 * (accepted and ignored by the API), a network error, an API outage — is
 * swallowed on purpose.
 */
export async function recordEventPageView(
  orgSlug: string,
  eventSlug: string,
  code: string | null,
): Promise<void> {
  try {
    await callBackend(
      `/api/v1/public/organizations/${encodeURIComponent(orgSlug)}/events/${encodeURIComponent(
        eventSlug,
      )}/page-views`,
      { method: "POST", body: JSON.stringify(code ? { code } : {}) },
    );
  } catch {
    // Display-only stats. A view that goes uncounted costs an organizer one
    // number; a view that throws would cost a buyer their page.
  }
}
