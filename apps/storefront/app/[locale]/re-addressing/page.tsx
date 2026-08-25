import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { ReAddressingLinkAccept } from "@/components/re-addressing-link-accept";
import { StorefrontShell } from "@/components/storefront-shell";
import { Link } from "@/i18n/navigation";
import { APIError, callBackend } from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import {
  reAddressingLinkState,
  reAddressingLinkToken,
  type ReAddressingLinkRead,
  type ReAddressingLinkView,
} from "@/lib/re-addressing-link";

// Rendered per request; nothing here may be prerendered or cached. The address
// carries a credential that moves a Sale and asserts who somebody is.
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: ReAddressingPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "reAddressingLink" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // NEVER INDEXED. The address carries a token that mints a Verified Customer
    // and moves a purchase to them; a crawler that filed it would put that
    // credential into a search index.
    robots: { index: false, follow: false },
  };
}

type ReAddressingPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string | string[] }>;
};

/**
 * Where a Re-addressing Link lands (#422, parent #419, ADR 0058).
 *
 * A Platform Operator recorded, on the organizer's request, the address a
 * stranded buyer meant; the platform mailed this link to it, and pressing the
 * button accepts. The press is Proof of Email Ownership by the standard ADR
 * 0035 set, so it mints or matches a Verified Customer, moves the Sale to them
 * and lands them signed in on it.
 *
 * THIS PAGE READS AND WRITES NOTHING. It takes the token out of the address,
 * asks the API for the link's VIEW — the Event, the reference, the corrected
 * address, and whether it has already been accepted — and renders. The view
 * endpoint is a GET that changes nothing, which is what lets a mail scanner
 * fetch this page before the buyer sees it and move no Sale. Everything else
 * happens in the client island below, on a press. Anything added here that
 * writes on render breaks that, and breaks it silently.
 *
 * IT IS THE ASSIGNMENT ACCEPT PAGE'S TWIN, WITH ONE DELIBERATE DIFFERENCE. That
 * page shows nothing before the press, because a known Customer's name before
 * the press would be an oracle for whether an address is registered. This one
 * shows the three facts the mail already carried and the API's view discloses
 * for exactly this purpose, so the buyer knows which purchase they are
 * accepting (user story 3) — and never the buyer's name, the money, the Tax ID
 * or the Tickets.
 *
 * A LINK THAT NO LONGER WORKS IS SAID PLAINLY, and the reasons are told apart:
 * this reader is the buyer, so a withdrawn re-addressing, a reversed Sale and a
 * started Event are their own facts. Each ends on a way back to the Storefront.
 *
 * A LINK ALREADY ACCEPTED STILL OFFERS THE BUTTON. A mail opened twice, or a
 * double press, must land on the Sale rather than on an error; the accept is
 * idempotent on the API's side and returns the same Sale and a session, so the
 * page says the purchase is already theirs and the button reads "open".
 *
 * NO SIGN-IN AND NO SESSION READ. The reader may have no account, and this is
 * what creates one. It is a localized page reached at the unprefixed
 * /re-addressing the link carries, and redirected into a language by the
 * middleware — which brings the query string with it, so the token survives
 * the hop. The link is written locale-free on purpose: the browser chooses.
 */
export default async function ReAddressingPage({ params, searchParams }: ReAddressingPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const { token } = await searchParams;
  const signedToken = reAddressingLinkToken(token);
  const state = reAddressingLinkState(
    signedToken,
    signedToken === "" ? null : await readReAddressingLink(signedToken),
  );

  const t = await getTranslations("reAddressingLink");

  return (
    // NO customerNav. The reader is not necessarily a Customer when they
    // arrive — they may become one by pressing the button — and an invitation
    // to sign in would suggest they need an account for something that
    // deliberately needs none.
    <StorefrontShell>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />
        {state.kind === "incomplete" ? (
          <>
            <Alert variant="destructive">
              <AlertTitle>{t("incompleteTitle")}</AlertTitle>
              <AlertDescription>{t("incompleteDescription")}</AlertDescription>
            </Alert>
            <p className="text-sm">
              <Link href="/" className="underline underline-offset-4">
                {t("backToStorefront")}
              </Link>
            </p>
          </>
        ) : state.kind === "failed" ? (
          <>
            <Alert variant="destructive">
              <AlertTitle>{t("failedLinkTitle")}</AlertTitle>
              <AlertDescription>{t(`${state.failure}Description`)}</AlertDescription>
            </Alert>
            <p className="text-sm">
              <Link href="/" className="underline underline-offset-4">
                {t("backToStorefront")}
              </Link>
            </p>
          </>
        ) : (
          <>
            {state.kind === "accepted" ? (
              <Alert>
                <AlertTitle>{t("acceptedTitle")}</AlertTitle>
                <AlertDescription>{t("acceptedDescription")}</AlertDescription>
              </Alert>
            ) : null}
            <dl className="space-y-3 rounded-lg border p-4 text-sm">
              <div className="space-y-1">
                <dt className="text-muted-foreground">{t("eventLabel")}</dt>
                <dd className="font-medium">{state.view.event_name}</dd>
              </div>
              <div className="space-y-1">
                <dt className="text-muted-foreground">{t("referenceLabel")}</dt>
                <dd className="font-mono font-medium">{state.view.confirmation_ref}</dd>
              </div>
              <div className="space-y-1">
                <dt className="text-muted-foreground">{t("addressLabel")}</dt>
                <dd className="font-medium">{state.view.corrected_email}</dd>
              </div>
            </dl>
            <ReAddressingLinkAccept token={signedToken} accepted={state.kind === "accepted"} />
          </>
        )}
      </div>
    </StorefrontShell>
  );
}

/**
 * The view read, as the rules module wants it: the view, or the API's refusal
 * with its code and details. A transport failure reads as a refusal with no
 * code, which the rules map to the honest floor; the reader is told the link
 * does not work rather than shown a stack trace.
 */
async function readReAddressingLink(token: string): Promise<ReAddressingLinkRead> {
  try {
    const envelope = await callBackend<ReAddressingLinkView>(
      `/api/v1/public/re-addressing-link?token=${encodeURIComponent(token)}`,
      { method: "GET" },
    );
    if (!envelope.data) {
      return { status: "error", code: undefined };
    }
    return { status: "ok", view: envelope.data };
  } catch (error) {
    if (error instanceof APIError) {
      return { status: "error", code: error.code, details: error.details };
    }
    return { status: "error", code: undefined };
  }
}
