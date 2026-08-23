import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Button, Card, CardContent } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { SignInToUndo, UndoWindowNotice } from "@/components/undo-window-notice";
import { getFormatLocale } from "@/i18n/format-locale.server";
import { Link, redirect } from "@/i18n/navigation";
import { getCheckoutReversal } from "@/lib/api";
import { readCheckoutContext } from "@/lib/checkout-context";
import { signInToTicketsHref } from "@/lib/checkout-return";
import { getCustomerSession } from "@/lib/customer-session";
import { undoDeadline } from "@/lib/undo-window";

export const dynamic = "force-dynamic";

type SuccessPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ ref?: string }>;
};

export async function generateMetadata({ params }: SuccessPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "checkout.success" });
  return {
    // The same words as the heading, from the same key: the tab and the page
    // are saying one thing.
    title: t("title"),
    robots: { index: false, follow: false },
  };
}

/**
 * The end of an approved checkout: the Sale Confirmation reference, front and
 * center, in the same monospaced treatment the Customer Area gives it. The
 * reference arrives as a query param from the return handler, so refreshing
 * this page re-renders the same confirmation — and re-walking the return leg
 * lands here again without touching the sale (confirm is idempotent).
 *
 * IT LEADS WITH THE CUSTOMER AREA, AND IT STILL WORKS SIGNED OUT (#387).
 * Checkout requires a Customer Session since ADR 0054, so the ordinary arrival
 * here is a signed-in buyer, and the first thing offered is their tickets rather
 * than an invitation to sign in for them.
 *
 * The signed-out branch stays, and it is not guest-checkout residue. The buyer
 * held a session when they pressed pay and then spent minutes off this origin at
 * the Payment Provider: a cleared jar, a provider webview that keeps its own
 * cookies, a session revoked from another device, a return in a different
 * browser. Any of those lands somebody who HAS ALREADY PAID on this page with
 * nothing, and this is the most consequential page in the Storefront precisely
 * because the money moved before it rendered. So a visitor with no session is
 * shown their confirmation and handed the way in: sign in with the address the
 * purchase was made under, which the checkout context remembers because the API
 * reported it (lib/checkout-return.ts). The Sale Confirmation in their inbox is
 * the other door, and the copy says so.
 */
export default async function CheckoutSuccessPage({ params, searchParams }: SuccessPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { ref } = await searchParams;
  const reference = ref?.trim();
  // No reference, no confirmation to show. The address is a stale bookmark, a
  // link whose query a scanner stripped, or one typed by hand — in every case
  // somebody arriving here has nothing to read, and the page has nothing to
  // recover it from: the reference lives in this URL and nowhere else on the
  // buyer's side.
  //
  // Home rather than a 404, which is the answer the leg before this one already
  // gives the same situation: /checkout/return sends a caller with no client
  // transaction id to "/" for exactly this reason. A hard 404 is defensible as
  // an address that names nothing, but the person reading it is usually a buyer
  // mid-purchase, and a dead end is the worst thing to hand them; the front door
  // at least leads somewhere. Their tickets are safe either way — the sale was
  // recorded before this page was ever reached, and the receipt carries the
  // reference by email.
  if (!reference) {
    redirect({ href: "/", locale });
  }

  const [context, session] = await Promise.all([readCheckoutContext(), getCustomerSession()]);
  const signedIn = session.status === "ok" && session.data.ticket_sale_id === null;

  // The Reversal Window, if this purchase has one on offer (#121, ADR 0018).
  //
  // Undoing requires a Customer Session, and the buyer reading this may no
  // longer hold one — not because checkout was guest-facing, which it no longer
  // is, but because theirs may not have survived the round trip through the
  // Payment Provider (#387). Saying nothing here is what sends them to email the
  // Organization instead, which is the outcome self-service was meant to remove.
  // So this page states the deadline and hands them the sign-in in one click;
  // the undo itself happens in the Customer Area, never here.
  //
  // Keyed on the checkout this browser began, out of the httpOnly context
  // cookie. Lose that cookie — another browser, a cleared jar — and the page
  // degrades exactly as it already does for the event link: the confirmation is
  // still a confirmation, it just says nothing about undoing.
  //
  // The API decides `reversible`, and it is not merely a clock: a sale settled
  // by a Payment Provider this deployment cannot ask comes back false inside its
  // own window, so this page never advertises a deadline for an undo that would
  // be refused.
  //
  // The answer also names the sale, which is the only way this page could know
  // it: a checkout knows its own client transaction id and nothing else. That is
  // what lets the link below land a buyer on their own purchase instead of on a
  // list of everything they have ever bought (#121).
  const reversal = context ? await getCheckoutReversal(context.clientTransactionId) : null;
  // The words of the deadline are the buyer's language; the clock behind it
  // stays Ecuador's, which is the rule's own (ADR 0018).
  const reversalDeadline = undoDeadline(reversal, await getFormatLocale());
  const t = await getTranslations("checkout.success");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <main className="mx-auto w-full max-w-xl px-4 py-12 sm:py-16">
        <div className="space-y-6 text-center">
          <div className="space-y-2">
            <h1 className="text-3xl font-semibold tracking-tight">{t("title")}</h1>
            <p className="text-muted-foreground">
              {/* The Event's name is the organizer's and is never translated;
                  only the sentence around it is, and the name goes inside that
                  sentence rather than in front of it. */}
              {context?.eventName
                ? t("confirmedEvent", { event: context.eventName })
                : t("confirmedGeneric")}
            </p>
          </div>

          <Card>
            <CardContent className="space-y-1 p-6">
              <p className="text-sm text-muted-foreground">{t("referenceLabel")}</p>
              <p className="font-mono text-3xl font-semibold tracking-wide" data-testid="confirmation-ref">
                {reference}
              </p>
            </CardContent>
          </Card>

          <p className="text-sm text-muted-foreground">{t("emailed")}</p>

          {/* Said only to the buyer who came back without a session, because it
              is the only one for whom reaching their tickets is not one click.
              It names both doors — signing in again, and the Sale Confirmation
              already in their inbox — since whichever failed them is exactly the
              one they cannot use. */}
          {signedIn ? null : (
            <p className="text-sm text-muted-foreground">{t("signedOutHint")}</p>
          )}

          {/* Changed your mind? Nothing here undoes anything — it names the
              deadline and offers the sign-in that leads to the purchase in the
              Customer Area, where the undo lives. Someone already signed in
              skips the prompt and goes straight to that sale. */}
          {reversalDeadline ? (
            <UndoWindowNotice deadline={reversalDeadline} className="rounded-lg border p-4 sm:p-5">
              <SignInToUndo
                signedIn={signedIn}
                email={context?.customerEmail ?? null}
                ticketSaleId={reversal?.ticket_sale_id ?? null}
              />
            </UndoWindowNotice>
          ) : null}

          <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
            {/* The tickets lead, always. A signed-in buyer — the ordinary case
                since ADR 0054 — goes straight to their Customer Area; one whose
                session did not survive the Payment Provider goes there through
                the sign-in, with the address the purchase was made under already
                in the field. It is the same offer either way, and the difference
                is a door rather than a dead end. */}
            <Button asChild className="h-11">
              {signedIn ? (
                <Link href="/tickets">{t("seeTickets")}</Link>
              ) : (
                <Link href={signInToTicketsHref(context?.customerEmail)}>
                  {t("signInToSeeTickets")}
                </Link>
              )}
            </Button>
            {context ? (
              <Button asChild variant="secondary" className="h-11">
                <Link href={context.eventPath}>{t("backToEvent")}</Link>
              </Button>
            ) : null}
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">{t("discoverMore")}</Link>
            </Button>
          </div>
        </div>
      </main>
    </StorefrontShell>
  );
}
