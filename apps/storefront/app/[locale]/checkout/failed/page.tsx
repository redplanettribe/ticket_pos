import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { Link } from "@/i18n/navigation";
import { readCheckoutContext } from "@/lib/checkout-context";

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
 * the event page, where checking out again begins a fresh Payment with a new
 * client transaction id — the failed attempt is spent and is never retried in
 * place.
 *
 * ?issue=support marks the one exception: the provider approved the charge but
 * the sale could not be recorded. Trying again there could charge twice, so
 * the copy sends the Customer to the organizer instead.
 */
export default async function CheckoutFailedPage({ params, searchParams }: FailedPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { issue } = await searchParams;
  const context = await readCheckoutContext();
  const retryHref = context?.eventPath ?? "/";
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
