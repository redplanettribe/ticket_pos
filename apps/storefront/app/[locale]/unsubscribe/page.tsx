import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { UnsubscribeConfirm } from "@/components/unsubscribe-confirm";
import { BRAND_NAME } from "@/lib/brand";

// Rendered per request; nothing here may be prerendered or cached, because the
// address carries one Customer's token.
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: UnsubscribePageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "unsubscribe" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // Never indexed. The address carries a signed token naming one person, and
    // a crawler that filed it would put a working opt-out for a stranger's mail
    // into a search index.
    robots: { index: false, follow: false },
  };
}

type UnsubscribePageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string | string[] }>;
};

/**
 * Where the unsubscribe link in a Follow Digest lands (#224, parent #215,
 * ADR 0030).
 *
 * THIS PAGE ACTS ON NOTHING. It reads the token out of the address, renders, and
 * stops. The unsubscribe happens only when the button below is pressed, which
 * sends a POST — and that is the acceptance criterion the whole feature was
 * designed around. Mail security scanners open every link in every message
 * before a human sees it, so a page that unsubscribed on being fetched would let
 * one silence everybody it protects, permanently and invisibly. Anything added
 * here that writes on render breaks that, and breaks it silently.
 *
 * NO SIGN-IN, AND NO SESSION READ. A Digest is read in a mail client months
 * after anybody last signed in; an opt-out gated behind a passcode would not be
 * an opt-out. The signed token is the whole authority, it names one Customer,
 * and the only thing it can do is set one reversible flag.
 *
 * It is a localized page like every other, reached at the unprefixed
 * /unsubscribe the Digest carries and redirected into a language by the
 * middleware — which brings the query string with it, so the token survives the
 * hop. That redirect is a GET of a page that does nothing, so a scanner
 * following it has still changed nothing.
 *
 * The token is passed to a client island rather than posted from here, because
 * the act must be the reader's press and a Server Action rendered on this page
 * would be one more thing that could be made to fire without one.
 */
export default async function UnsubscribePage({ params, searchParams }: UnsubscribePageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const { token } = await searchParams;
  // A repeated parameter is not a link this platform ever wrote. Taking the
  // first would be guessing at which one somebody meant.
  const signedToken = typeof token === "string" ? token : "";

  const t = await getTranslations("unsubscribe");

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
          <UnsubscribeConfirm token={signedToken} />
        )}
      </div>
    </StorefrontShell>
  );
}
