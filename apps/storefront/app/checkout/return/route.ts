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
 *
 * IT ASKS FOR NO SESSION, AND ADR 0054 DID NOT CHANGE THAT (#387). Checkout now
 * stands behind a Customer Session, but this leg does not, and confirm stays
 * public for the same reason it always was: it runs after the money has moved,
 * on a request the Payment Provider built, in a browser that may have lost
 * everything in between. Requiring a session here would mean a cleared cookie
 * jar or a provider webview could leave a paid-for Payment unconfirmed — which
 * is the one failure this system must not be able to produce. What the buyer
 * lost is answered on the terminal page, not on this hop.
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

/**
 * Relative Location, resolved by the browser against the URL it asked for (see tickets/confirm).
 *
 * Vary names both inputs the language above was chosen from, for the reason
 * middleware.ts sets the same header: a cache keyed on this URL alone would hand
 * the next buyer the previous buyer's language, and here that happens on the leg
 * that lands after a real payment.
 *
 * Both are named even though the checkout context cookie usually decides alone.
 * The tempting narrower answer — Cookie, since the buyer's own language came
 * back with them — is correct only while that cookie survives; when it is gone,
 * damaged, or older than the field, Accept-Language picks the page, and a Vary
 * that omits it is wrong in exactly the case the fallback exists for.
 */
function localizedRedirect(locale: AppLocale, path: string): NextResponse {
  return new NextResponse(null, {
    status: 303,
    headers: { Location: localizedPath(locale, path), Vary: "Accept-Language, Cookie" },
  });
}
