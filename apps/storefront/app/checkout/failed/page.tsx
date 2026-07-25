import type { Metadata } from "next";
import Link from "next/link";

import { Alert, AlertDescription, AlertTitle, Button, StorefrontShell } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { readCheckoutContext } from "@/lib/checkout-context";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Payment not completed",
  robots: { index: false, follow: false },
};

type FailedPageProps = {
  searchParams: Promise<{ issue?: string }>;
};

/**
 * The end of a payment that did not become a sale. "Try again" goes back to
 * the event page, where checking out again begins a fresh Payment with a new
 * client transaction id — the failed attempt is spent and is never retried in
 * place.
 *
 * ?issue=support marks the one exception: the provider approved the charge but
 * the sale could not be recorded. Trying again there could charge twice, so
 * the copy sends the Customer to the organizer instead.
 */
export default async function CheckoutFailedPage({ searchParams }: FailedPageProps) {
  const { issue } = await searchParams;
  const context = await readCheckoutContext();
  const retryHref = context?.eventPath ?? "/";

  if (issue === "support") {
    return (
      <StorefrontShell customerNav={<HeaderCustomerNav />}>
        <main className="mx-auto w-full max-w-xl px-4 py-12 sm:py-16">
          <div className="space-y-6 text-center">
            <h1 className="text-3xl font-semibold tracking-tight">Something went wrong</h1>
            <Alert variant="destructive" className="text-left">
              <AlertTitle>Your payment may have gone through</AlertTitle>
              <AlertDescription>
                We couldn&apos;t record your tickets after the payment step. Please don&apos;t pay
                again — contact the event organizer and we&apos;ll sort it out.
              </AlertDescription>
            </Alert>
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">Back to Discover events</Link>
            </Button>
          </div>
        </main>
      </StorefrontShell>
    );
  }

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <main className="mx-auto w-full max-w-xl px-4 py-12 sm:py-16">
        <div className="space-y-6 text-center">
          <div className="space-y-2">
            <h1 className="text-3xl font-semibold tracking-tight">Payment not completed</h1>
            <p className="text-muted-foreground">
              {issue === "error"
                ? "We couldn't finish confirming your payment. You haven't been charged, and no tickets were issued."
                : "Your payment was declined or cancelled. You haven't been charged, and no tickets were issued."}
            </p>
          </div>

          <p className="text-sm text-muted-foreground">
            Your tickets weren&apos;t reserved, so if you&apos;d still like to go, just start again.
          </p>

          <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Button asChild className="h-11">
              <Link href={retryHref}>Try again</Link>
            </Button>
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">Discover more events</Link>
            </Button>
          </div>
        </div>
      </main>
    </StorefrontShell>
  );
}
