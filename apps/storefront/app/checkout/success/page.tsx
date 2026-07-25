import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Button, Card, CardContent, StorefrontShell } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { readCheckoutContext } from "@/lib/checkout-context";
import { getCustomerSession } from "@/lib/customer-session";

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
