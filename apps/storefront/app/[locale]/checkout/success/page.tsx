import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Button, Card, CardContent } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { SignInToUndo, UndoWindowNotice } from "@/components/undo-window-notice";
import { Link } from "@/i18n/navigation";
import { getCheckoutReversal } from "@/lib/api";
import { readCheckoutContext } from "@/lib/checkout-context";
import { getCustomerSession } from "@/lib/customer-session";
import { intlLocale, toAppLocale } from "@/lib/locale";
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
 */
export default async function CheckoutSuccessPage({ params, searchParams }: SuccessPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { ref } = await searchParams;
  const reference = ref?.trim();
  if (!reference) {
    notFound();
  }

  const [context, session] = await Promise.all([readCheckoutContext(), getCustomerSession()]);
  const signedIn = session.status === "ok" && session.data.ticket_sale_id === null;

  // The Reversal Window, if this purchase has one on offer (#121, ADR 0018).
  //
  // Checkout is guest-facing — nobody has to sign in to buy — so the buyer most
  // likely to change their mind is the one least equipped to act on it: undoing
  // requires a Customer Session. Saying nothing here is what sends them to email
  // the Organization instead, which is the outcome self-service was meant to
  // remove. So this page states the deadline and hands them the sign-in in one
  // click; the undo itself happens in the Customer Area, never here.
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
  const reversalDeadline = undoDeadline(reversal, intlLocale(toAppLocale(locale)));
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
            {signedIn ? (
              <Button asChild className="h-11">
                <Link href="/tickets">{t("seeTickets")}</Link>
              </Button>
            ) : null}
            {context ? (
              <Button asChild variant={signedIn ? "secondary" : "default"} className="h-11">
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
