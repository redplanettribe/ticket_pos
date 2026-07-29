import { NextResponse } from "next/server";

import { APIError, confirmCheckout } from "@/lib/api";
import { parseProviderReturn } from "@/lib/checkout";
import { readCheckoutLocale } from "@/lib/checkout-context";
import { localizedPath, type AppLocale } from "@/lib/locale";
import { redirectLocale } from "@/lib/redirect-locale";

// Confirms a Payment; never cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Where the Payment Provider's return redirect lands (ADR 0008: nothing
 * external may address the Go API, so the provider sends the Customer here and
 * THIS handler asks the API to confirm, relaying the provider's query params
 * verbatim as provider_params).
 *
 * A route handler rather than a page for the same reason the Confirmation Link
 * handler is one: it is a navigation whose only job is a server-side call and
 * a redirect — the Customer never sees it.
 *
 * Safe on refresh by construction: confirm is idempotent on the API side
 * (keyed by our client transaction id), so re-hitting this URL re-reads the
 * recorded outcome and lands on the same terminal page. It never duplicates a
 * sale.
 */
export async function GET(request: Request) {
  // The Payment Provider built this URL from a constant it was handed when the
  // Payment began, so it names no language and this handler chooses one for the
  // terminal page. The buyer's own came back with them in the checkout context;
  // only when that is gone, damaged, or older than the field does this fall back
  // to guessing from the switcher's cookie and Accept-Language — which is what a
  // buyer reading /es on an English-language browser used to get after paying.
  const locale = (await readCheckoutLocale()) ?? (await redirectLocale());
  const redirectTo = (path: string) => localizedRedirect(locale, path);

  const { clientTransactionId, providerParams } = parseProviderReturn(
    new URL(request.url).searchParams,
  );
  if (!clientTransactionId) {
    return redirectTo("/");
  }

  try {
    const result = await confirmCheckout(clientTransactionId, providerParams);
    if (result.status === "approved" && result.confirmation_ref) {
      return redirectTo(`/checkout/success?ref=${encodeURIComponent(result.confirmation_ref)}`);
    }
    return redirectTo("/checkout/failed");
  } catch (error) {
    if (error instanceof APIError && error.code === "PAYMENT_NOT_FOUND") {
      // A return leg for a Payment that does not exist is a stale or
      // hand-crafted URL; there is nothing to settle and nothing to show.
      return redirectTo("/");
    }
    if (error instanceof APIError && error.code === "PAYMENT_SALE_COMMIT_FAILED") {
      // The provider approved the charge but the sale could not be recorded —
      // the one case where "try again" would be exactly wrong.
      return redirectTo("/checkout/failed?issue=support");
    }
    return redirectTo("/checkout/failed?issue=error");
  }
}

/** Relative Location, resolved by the browser against the URL it asked for (see tickets/confirm). */
function localizedRedirect(locale: AppLocale, path: string): NextResponse {
  return new NextResponse(null, { status: 303, headers: { Location: localizedPath(locale, path) } });
}
