import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { AssignmentLinkAccept } from "@/components/assignment-link-accept";
import { StorefrontShell } from "@/components/storefront-shell";
import { BRAND_NAME } from "@/lib/brand";

// Rendered per request; nothing here may be prerendered or cached. The address
// carries a credential — and the strongest one this platform mails, since what
// it does is assert who somebody is.
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: AcceptPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "assignmentLink" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // NEVER INDEXED, and this one matters more than any other page in the app.
    // The address carries a token that MINTS A VERIFIED CUSTOMER; a crawler that
    // filed it would put an identity-minting credential for a stranger's inbox
    // into a search index.
    robots: { index: false, follow: false },
  };
}

type AcceptPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string | string[] }>;
};

/**
 * Where an Assignment Link lands (#325, parent #322, ADR 0046).
 *
 * A friend bought a ticket and gave this address for it; the platform mailed
 * this link, and pressing it accepts. The press is Proof of Email Ownership by
 * the standard ADR 0035 set — "clicking from the inbox being itself proof of
 * ownership" — so it mints or matches a Verified Customer, and only THEN is the
 * reader asked their name and the Ticket Questions.
 *
 * THIS PAGE ACTS ON NOTHING. It reads the token out of the address, renders, and
 * stops. Everything happens in the client island below, on a press. Mail
 * security scanners open every link in every message before a human sees it, and
 * a page that accepted on render would let one mint a Customer nobody proved and
 * disclose a stranger's address to an Organization. Anything added here that
 * writes on render breaks that, and breaks it silently.
 *
 * IT ALSO SHOWS NOTHING ABOUT THE TICKET BEFORE THE PRESS, which is the second
 * half of the same discipline. Fetching the Event and the Ticket Type here would
 * mean an endpoint that answers before anybody has proved anything — and one
 * that prefilled a known Customer's name would be an oracle for whether an
 * address is registered (ADR 0035). What tells the reader what they have is the
 * MAIL: it names the Event and the Ticket Type, states that the address was
 * given by whoever bought the ticket, and says what accepting discloses.
 *
 * NO SIGN-IN AND NO SESSION READ. The reader has no account, and this is what
 * creates one. Asking them to sign in would be asking somebody to prove an
 * address in order to be allowed to prove an address.
 *
 * It is a localized page reached at the unprefixed /accept the link carries, and
 * redirected into a language by the middleware — which brings the query string
 * with it, so the token survives the hop, and which is a GET of a page that does
 * nothing, so a scanner following it has accepted nothing. The link is written
 * locale-free on purpose: the reader is not the buyer and may not share their
 * language, so the browser chooses rather than the purchase.
 *
 * The address is /accept and never /confirm-anything: the glossary already
 * carries four confirmations, and ADR 0046 reserves this act the word `accept`.
 */
export default async function AcceptPage({ params, searchParams }: AcceptPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const { token } = await searchParams;
  // A repeated parameter is not a link this platform ever wrote. Taking the
  // first would be guessing at which one somebody meant.
  const signedToken = typeof token === "string" ? token : "";

  const t = await getTranslations("assignmentLink");

  return (
    // NO customerNav. The reader is not a Customer when they arrive — they
    // become one by pressing the button — and an invitation to sign in would
    // suggest they need an account for something that deliberately needs none.
    <StorefrontShell>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />
        {signedToken === "" ? (
          // A link that arrived without its token — truncated by a mail client,
          // or copied by hand. There is nothing to accept and nothing to offer:
          // this reader does not know who bought the ticket, so the page cannot
          // even send them back to whoever could help.
          <Alert variant="destructive">
            <AlertTitle>{t("incompleteTitle")}</AlertTitle>
            <AlertDescription>{t("incompleteDescription")}</AlertDescription>
          </Alert>
        ) : (
          <AssignmentLinkAccept token={signedToken} />
        )}
      </div>
    </StorefrontShell>
  );
}
