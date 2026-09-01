import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { getLocale } from "next-intl/server";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import { storefrontTermsUrl } from "@/lib/storefront";
import { RETURN_PARAM, termsGateReturnPath } from "@/lib/terms-gate";

import { TermsGateForm } from "./terms-gate-form";

import type { AppLocale } from "@ticket-pos/locale";

/**
 * The interstitial a live Staff Session meets when a gating edition arrives
 * (#570, ADR 0067's amendment to ADR 0066).
 *
 * ADR 0066 had the gate bind at the next sign-in, which for a daily user is
 * never: the session's expiry slides on every authenticated request. So the
 * gate binds on the next PAGE NAVIGATION instead, and this is the page it binds
 * to. The middleware diverts here with `next` naming where the person was
 * going; accepting sends them back.
 *
 * WHAT THIS PAGE DOES NOT DO: it does not sign anybody out, does not re-mint a
 * session and does not spend a passcode. The person walked in signed in and
 * walks out signed in, with one acceptance recorded in between. Passcodes are
 * rationed — 10 per IP per 15 minutes, sharing a platform ceiling with Sale
 * Confirmations — so making somebody re-prove an address they have already
 * proven, mid-event, for a document they have not read yet, is precisely the
 * cost this design refuses to impose.
 *
 * THE BOX IS THE BACKEND'S, not this app's. The words beside the checkbox are
 * the published artifact rendered verbatim, and the language they were ACTUALLY
 * served in comes back with them: an edition that publishes no translation in
 * the reader's language is floored at the prevailing Spanish text (§37), and
 * the link beside the box then opens the Spanish page rather than one that is
 * not there. This page never decides that — identity/service's termsGateLabels
 * does, once, for both this surface and the sign-in door.
 *
 * And it can never degrade to a card with no acceptance control: the read
 * either returns a box with a token, or it throws. The backend refuses to state
 * a contract it cannot word, and an error page is the honest answer to that —
 * a page that looks like a way past the gate and is not would be the one
 * unacceptable outcome.
 */

type TermsGateData = {
  outstanding: boolean;
  terms_required: {
    pending_terms_token: string;
    expires_at: string;
    version: string;
    acceptance_label: string;
    /**
     * The second box's words, ABSENT from the payload when the edition in
     * effect carries no `label-adulthood-declaration` artifact (#587,
     * ADR 0069). Optional here for exactly that reason: its presence is the
     * answer to "does this edition ask?", and the backend refuses to serve this
     * gate at all when the edition asks and cannot word the box — so there is
     * no state in which this arrives empty and a box is still owed.
     */
    adulthood_declaration_label?: string;
    label_locale: string;
  } | null;
};

type TermsGatePageProps = {
  searchParams: Promise<{ [RETURN_PARAM]?: string | string[] }>;
};

export default async function TermsGatePage({ searchParams }: TermsGatePageProps) {
  const params = await searchParams;
  const requested = params[RETURN_PARAM];
  const returnPath = termsGateReturnPath(Array.isArray(requested) ? requested[0] : requested);

  const sessionToken = (await cookies()).get(SESSION_COOKIE_NAME)?.value;
  if (!sessionToken) {
    // Only reachable if the cookie died between the middleware's check and this
    // render. The sign-in page is where that recovers, and this page has
    // nothing to show somebody it cannot name.
    redirect("/login");
  }

  const locale = (await getLocale()) as AppLocale;
  const envelope = await callBackend<TermsGateData>(
    `/api/v1/staff/terms/gate?locale=${encodeURIComponent(locale)}`,
    { sessionToken },
  );
  const required = envelope.data?.terms_required ?? null;

  if (!envelope.data?.outstanding || !required) {
    // Accepted in another tab, or the edition was withdrawn while this page was
    // being asked for. Nothing is owed, so nothing is asked.
    redirect(returnPath);
  }
  if (!required.acceptance_label || !required.pending_terms_token) {
    throw new Error(
      "the Terms gate returned no acceptance control; refusing to render an interstitial " +
        "somebody cannot answer",
    );
  }

  return (
    <TermsGateForm
      acceptanceLabel={required.acceptance_label}
      // Present iff this edition asks (#587). Passed straight through and never
      // defaulted to a catalog string: the words are evidence, hashed into the
      // edition's fingerprint, and the box is drawn only where they are.
      adulthoodDeclarationLabel={required.adulthood_declaration_label ?? null}
      gateToken={required.pending_terms_token}
      // The document the words came from — the reader's language in the
      // ordinary case, the prevailing one when this edition does not publish
      // theirs. A link across, never a link to a page that is not there.
      termsUrl={storefrontTermsUrl((required.label_locale || locale) as AppLocale)}
      returnPath={returnPath}
    />
  );
}
