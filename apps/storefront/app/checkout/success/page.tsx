import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Button, Card, CardContent, StorefrontShell } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { SignInToUndo, UndoWindowNotice } from "@/components/undo-window-notice";
import { getCheckoutReversal } from "@/lib/api";
import { readCheckoutContext } from "@/lib/checkout-context";
import { getCustomerSession } from "@/lib/customer-session";
import { undoDeadline } from "@/lib/undo-window";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "You're going!",
  robots: { index: false, follow: false },
};

type SuccessPageProps = {
  searchParams: Promise<{ ref?: string }>;
};

/**
 * The end of an approved checkout: the Sale Confirmation reference, front and
 * center, in the same monospaced treatment the Customer Area gives it. The
 * reference arrives as a query param from the return handler, so refreshing
 * this page re-renders the same confirmation — and re-walking the return leg
 * lands here again without touching the sale (confirm is idempotent).
 */
export default async function CheckoutSuccessPage({ searchParams }: SuccessPageProps) {
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
  const reversal = context ? await getCheckoutReversal(context.clientTransactionId) : null;
  const reversalDeadline = undoDeadline(reversal);

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <main className="mx-auto w-full max-w-xl px-4 py-12 sm:py-16">
        <div className="space-y-6 text-center">
          <div className="space-y-2">
            <h1 className="text-3xl font-semibold tracking-tight">You&apos;re going!</h1>
            <p className="text-muted-foreground">
              {context?.eventName
                ? `Your tickets for ${context.eventName} are confirmed.`
                : "Your payment was approved and your tickets are confirmed."}
            </p>
          </div>

          <Card>
            <CardContent className="space-y-1 p-6">
              <p className="text-sm text-muted-foreground">Confirmation reference</p>
              <p className="font-mono text-3xl font-semibold tracking-wide" data-testid="confirmation-ref">
                {reference}
              </p>
            </CardContent>
          </Card>

          <p className="text-sm text-muted-foreground">
            We&apos;ve emailed your Sale Confirmation with a link to your tickets. Quote the
            reference above at the door if you need to.
          </p>

          {/* Changed your mind? Nothing here undoes anything — it names the
              deadline and offers the sign-in that leads to the Customer Area,
              where the undo lives. Someone already signed in skips the prompt
              and goes straight there. */}
          {reversalDeadline ? (
            <UndoWindowNotice deadline={reversalDeadline} className="rounded-lg border p-4 sm:p-5">
              <SignInToUndo signedIn={signedIn} email={context?.customerEmail ?? null} />
            </UndoWindowNotice>
          ) : null}

          <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
            {signedIn ? (
              <Button asChild className="h-11">
                <Link href="/tickets">See your tickets</Link>
              </Button>
            ) : null}
            {context ? (
              <Button asChild variant={signedIn ? "secondary" : "default"} className="h-11">
                <Link href={context.eventPath}>Back to the event</Link>
              </Button>
            ) : null}
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">Discover more events</Link>
            </Button>
          </div>
        </div>
      </main>
    </StorefrontShell>
  );
}
