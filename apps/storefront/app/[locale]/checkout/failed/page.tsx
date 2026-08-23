import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { Link } from "@/i18n/navigation";
import { readCheckoutContext } from "@/lib/checkout-context";
import { retryCheckoutPath } from "@/lib/checkout-signin";

export const dynamic = "force-dynamic";

type FailedPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ issue?: string }>;
};

export async function generateMetadata({ params }: FailedPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "checkout.failed" });
  return {
    // The ordinary outcome's heading, from the same key it renders from. The
    // support case is a different page state, not a different address, so the
    // tab keeps saying the thing this route is about.
    title: t("title"),
    robots: { index: false, follow: false },
  };
}

/**
 * The end of a payment that did not become a sale. "Try again" goes back to
 * the event page — carrying the selection that was being paid for, so a
 * declined card does not also cost the buyer their basket — where checking out
 * again begins a fresh Payment with a new client transaction id: the failed
 * attempt is spent and is never retried in place.
 *
 * ?issue=support marks the one exception: the provider approved the charge but
 * the sale could not be recorded. Trying again there could charge twice, so
 * the copy sends the Customer to the organizer instead.
 *
 * NOTHING HERE READS A SESSION, AND THAT IS DELIBERATE (#387). A buyer can come
 * back from the Payment Provider without one — a cleared jar, a provider webview
 * that drops cookies, a return in a different browser — and this page must land
 * correctly for them: the outcome of a payment is not a fact about who is
 * signed in. The Event page the retry points at is public and re-judges the
 * selection on arrival, and if their session really is gone, the wall at Buy
 * (ADR 0054, #385) meets them there and brings them back with the basket
 * intact. One wall, in one place, and this is not it.
 */
export default async function CheckoutFailedPage({ params, searchParams }: FailedPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { issue } = await searchParams;
  const context = await readCheckoutContext();
  // Back to the Event with the basket that was about to be paid for (ADR 0054,
  // #385). The quantities are a suggestion the page re-judges on arrival, so a
  // Ticket Type that sold out while the buyer was at the provider is reported
  // rather than restored — and the dialog does NOT reopen: a card was just
  // refused, and pressing Buy again is the buyer's to do.
  const retryHref = context ? retryCheckoutPath(context.eventPath, context.selection) : "/";
  const t = await getTranslations("checkout.failed");

  if (issue === "support") {
    return (
      <StorefrontShell customerNav={<HeaderCustomerNav />}>
        <main className="mx-auto w-full max-w-xl px-4 py-12 sm:py-16">
          <div className="space-y-6 text-center">
            <h1 className="text-3xl font-semibold tracking-tight">{t("supportTitle")}</h1>
            <Alert variant="destructive" className="text-left">
              <AlertTitle>{t("supportAlertTitle")}</AlertTitle>
              <AlertDescription>{t("supportAlertBody")}</AlertDescription>
            </Alert>
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">{t("backToDiscover")}</Link>
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
            <h1 className="text-3xl font-semibold tracking-tight">{t("title")}</h1>
            <p className="text-muted-foreground">
              {issue === "error" ? t("unconfirmed") : t("declined")}
            </p>
          </div>

          <p className="text-sm text-muted-foreground">{t("retryHint")}</p>

          <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Button asChild className="h-11">
              <Link href={retryHref}>{t("tryAgain")}</Link>
            </Button>
            <Button asChild variant="ghost" className="h-11">
              <Link href="/">{t("discoverMore")}</Link>
            </Button>
          </div>
        </div>
      </main>
    </StorefrontShell>
  );
}
