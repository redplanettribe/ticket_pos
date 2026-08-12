import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  PageHeader,
} from "@ticket-pos/ui";

import { ConsentConfirm } from "@/components/consent-confirm";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { BRAND_NAME } from "@/lib/brand";

// Rendered per request; nothing here may be prerendered or cached, because the
// address carries one Customer's token.
export const dynamic = "force-dynamic";

export async function generateMetadata({
  params,
}: ConfirmConsentPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "confirmConsent" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // Never indexed. The address carries a signed token naming one person, and a
    // crawler that filed it would put a working consent grant for a stranger's
    // mail into a search index.
    robots: { index: false, follow: false },
  };
}

type ConfirmConsentPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string | string[] }>;
};

/**
 * Where the confirmation link in a Sale Confirmation lands (#255, parent #249,
 * ADR 0035).
 *
 * THIS PAGE ACTS ON NOTHING. It reads the token out of the address, renders, and
 * stops. The confirmation happens only when the button below is pressed, which
 * sends a POST — and that is the acceptance criterion the whole design turns on.
 * Mail security scanners open every link in every message before a human sees
 * it, so a page that confirmed on being fetched would let one manufacture the
 * very consent this feature exists to prove was given. Anything added here that
 * writes on render breaks that, and breaks it silently.
 *
 * NO SIGN-IN, AND NO SESSION READ. The person reading this may have no account:
 * a guest checkout creates a Customer nobody has ever signed in as, and pressing
 * a link that only ever travelled to this address is itself the proof of
 * ownership that the guest's tick lacked.
 *
 * It is a localized page like every other, reached at the unprefixed
 * /confirm-consent the receipt carries and redirected into a language by the
 * middleware — which brings the query string with it, so the token survives the
 * hop. That redirect is a GET of a page that does nothing, so a scanner
 * following it has still confirmed nothing.
 *
 * The token is passed to a client island rather than posted from here, because
 * the act must be the reader's press and a Server Action rendered on this page
 * would be one more thing that could be made to fire without one.
 */
export default async function ConfirmConsentPage({
  params,
  searchParams,
}: ConfirmConsentPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const { token } = await searchParams;
  // A repeated parameter is not a link this platform ever wrote. Taking the
  // first would be guessing at which one somebody meant.
  const signedToken = typeof token === "string" ? token : "";

  const t = await getTranslations("confirmConsent");

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />

        {signedToken === "" ? (
          // A link that arrived without its token — truncated by a mail client,
          // or copied by hand. Nothing to confirm, so nothing is offered to
          // press; the Customer Area is the way through.
          <Alert variant="destructive">
            <AlertTitle>{t("missingTokenTitle")}</AlertTitle>
            <AlertDescription>{t("missingTokenDescription")}</AlertDescription>
          </Alert>
        ) : (
          <ConsentConfirm token={signedToken} />
        )}
      </div>
    </StorefrontShell>
  );
}
