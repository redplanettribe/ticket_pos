import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Badge, Card, CardContent, CardDescription, CardHeader, CardTitle, buttonVariants, cn } from "@ticket-pos/ui";

import { parseStubPaymentRequest, stubOutcomeURL } from "@/lib/checkout";
import { formatPrice } from "@/lib/format";
import { stubPaymentsActive } from "@/lib/stub-payments";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Payment — test mode",
  robots: { index: false, follow: false },
};

type StubPageProps = {
  searchParams: Promise<{
    client_transaction_id?: string;
    amount_cents?: string;
    currency?: string;
    response_url?: string;
  }>;
};

/**
 * The stub Payment Provider's "hosted payment page" (contract in
 * backend/internal/platform/payment.go): a dev-only interstitial standing in
 * for PayPhone, opened by a top-level redirect exactly like the real page
 * would be. It shows the amount and offers Approve and Decline, each a plain
 * navigation to the response URL with the contract params appended — driving
 * the same return-redirect legs a real provider drives.
 *
 * Guarded by stubPaymentsActive() (see lib/stub-payments.ts): on a production
 * build without the explicit STOREFRONT_STUB_PAYMENTS=1 opt-in, this route is
 * a 404 and the interstitial does not exist.
 */
export default async function StubPaymentPage({ searchParams }: StubPageProps) {
  if (!stubPaymentsActive()) {
    notFound();
  }

  const request = parseStubPaymentRequest(await searchParams);
  if (!request) {
    notFound();
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/40 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-2">
          <Badge variant="secondary" className="w-fit">
            Test payment — no money moves
          </Badge>
          <CardTitle className="text-xl">Confirm your payment</CardTitle>
          <CardDescription>
            This page stands in for the payment provider during development.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div>
            <p className="text-sm text-muted-foreground">Amount</p>
            <p className="text-3xl font-semibold" data-testid="stub-amount">
              {formatPrice(request.amountCents, request.currency)}
            </p>
          </div>
          <div className="space-y-2">
            {/* Anchors, not buttons: each verdict is a navigation to the
                response URL, exactly like a real provider's redirect. */}
            <a
              href={stubOutcomeURL(request.responseUrl, request.clientTransactionId, "approved")}
              className={cn(buttonVariants({ size: "lg" }), "h-11 w-full")}
            >
              Approve payment
            </a>
            <a
              href={stubOutcomeURL(request.responseUrl, request.clientTransactionId, "declined")}
              className={cn(buttonVariants({ variant: "outline", size: "lg" }), "h-11 w-full")}
            >
              Decline payment
            </a>
          </div>
          <p className="break-all text-xs text-muted-foreground">
            Reference: <span className="font-mono">{request.clientTransactionId}</span>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
