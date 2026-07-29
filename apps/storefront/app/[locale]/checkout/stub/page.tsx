import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";

import { Badge, Card, CardContent, CardDescription, CardHeader, CardTitle, buttonVariants, cn } from "@ticket-pos/ui";

import { parseStubPaymentRequest, stubOutcomeURL } from "@/lib/checkout";
import { formatPrice } from "@/lib/format";
import { intlLocale, toAppLocale } from "@/lib/locale";
import { stubPaymentsActive } from "@/lib/stub-payments";

export const dynamic = "force-dynamic";

type StubPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{
    client_transaction_id?: string;
    amount_cents?: string;
    currency?: string;
    response_url?: string;
  }>;
};

export async function generateMetadata({ params }: StubPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "checkout.stub" });
  return {
    title: t("metaTitle"),
    robots: { index: false, follow: false },
  };
}

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
export default async function StubPaymentPage({ params, searchParams }: StubPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  if (!stubPaymentsActive()) {
    notFound();
  }

  const request = parseStubPaymentRequest(await searchParams);
  if (!request) {
    notFound();
  }

  const t = await getTranslations("checkout.stub");

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/40 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-2">
          <Badge variant="secondary" className="w-fit">
            {t("badge")}
          </Badge>
          <CardTitle className="text-xl">{t("title")}</CardTitle>
          <CardDescription>{t("description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div>
            <p className="text-sm text-muted-foreground">{t("amountLabel")}</p>
            <p className="text-3xl font-semibold" data-testid="stub-amount">
              {formatPrice(request.amountCents, request.currency, intlLocale(toAppLocale(locale)))}
            </p>
          </div>
          <div className="space-y-2">
            {/* Anchors, not buttons: each verdict is a navigation to the
                response URL, exactly like a real provider's redirect. */}
            <a
              href={stubOutcomeURL(request.responseUrl, request.clientTransactionId, "approved")}
              className={cn(buttonVariants({ size: "lg" }), "h-11 w-full")}
            >
              {t("approve")}
            </a>
            <a
              href={stubOutcomeURL(request.responseUrl, request.clientTransactionId, "declined")}
              className={cn(buttonVariants({ variant: "outline", size: "lg" }), "h-11 w-full")}
            >
              {t("decline")}
            </a>
          </div>
          <p className="break-all text-xs text-muted-foreground">
            {/* The id is monospaced inside the sentence rather than appended
                after it: the label does not sit in front of the value in every
                language. */}
            {t.rich("reference", {
              reference: request.clientTransactionId,
              id: (chunks) => <span className="font-mono">{chunks}</span>,
            })}
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
