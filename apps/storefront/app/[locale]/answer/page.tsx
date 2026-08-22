import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { AnswerLinkForm } from "@/components/answer-link-form";
import { StorefrontShell } from "@/components/storefront-shell";
import { APIError, callBackend } from "@/lib/api";
import { answerLinkFailure, type AnswerLinkView } from "@/lib/answer-link";
import { BRAND_NAME } from "@/lib/brand";

// Rendered per request; nothing here may be prerendered or cached. The address
// carries a credential, and what it opens changes the moment somebody answers.
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: AnswerPageProps): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: "answerLink" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // NEVER INDEXED, and this one matters more than most. The address carries a
    // signed token that opens one Ticket's questions and takes an answer with no
    // sign-in; a crawler that filed it would put a working answering credential
    // for a stranger into a search index.
    robots: { index: false, follow: false },
  };
}

type AnswerPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string | string[] }>;
};

/**
 * Where an Answer Link lands (#312, ADR 0044).
 *
 * A signed, stateless, per-Ticket link that the buyer forwards to whoever will
 * hold that ticket. It opens ONE Ticket's Ticket Questions and nothing else.
 *
 * IT ASKS FOR NO PROOF OF IDENTITY. No sign-in, no passcode, no session, and
 * holding it makes nobody a Customer. That is the decision and not an oversight:
 * proving an email means collecting an email, and the person reading this never
 * came to this platform and gave it no address. A wrong Answer is cheap, and
 * both the buyer and Event Staff can fix one.
 *
 * IT SHOWS THE EVENT, THE TICKET TYPE AND THE QUESTIONS, AND NOTHING ELSE —
 * never the buyer's name or email, the price, the Tax ID, the Sale Confirmation
 * reference, or the Sale's other Tickets. This link gets pasted into group
 * chats, and what it discloses is the whole of its security. That is enforced at
 * the API (service.AnswerLinkView, with an integration test over the raw
 * response body); this page cannot widen it because there is nothing here to
 * widen — everything drawn below came out of those three fields.
 *
 * THE PAGE READS ON RENDER, WHICH IS SAFE HERE AND WOULD NOT BE ELSEWHERE. The
 * consent confirmation page deliberately acts on nothing when fetched, because a
 * mail scanner opening it would manufacture a consent. This one only READS: a
 * scanner that fetched it has answered nothing, and answering is the button
 * below, which is a PUT from the reader's own press.
 *
 * It is a localized page reached at the unprefixed /answer the link carries, and
 * redirected into a language by the middleware — which brings the query string
 * with it, so the token survives the hop. The link is written locale-free on
 * purpose: it is pasted by hand into a chat, and baking the buyer's Locale into
 * it would hand a Spanish-speaking holder the buyer's English page.
 */
export default async function AnswerPage({ params, searchParams }: AnswerPageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const { token } = await searchParams;
  // A repeated parameter is not a link this platform ever wrote. Taking the
  // first would be guessing at which one somebody meant.
  const signedToken = typeof token === "string" ? token : "";

  const t = await getTranslations("answerLink");

  if (signedToken === "") {
    // A link that arrived without its token — truncated by a chat app's link
    // detector, or copied by hand. There is nothing to open and nothing to
    // offer: the recovery is the person who sent it, and this page has no way
    // to reach them, which is the cost of holding no address for anybody here.
    return (
      <AnswerShell title={t("title")} description={t("description")}>
        <Alert variant="destructive">
          <AlertTitle>{t("incompleteTitle")}</AlertTitle>
          <AlertDescription>{t("incompleteDescription")}</AlertDescription>
        </Alert>
      </AnswerShell>
    );
  }

  let view: AnswerLinkView;
  try {
    // Server-side, like every other call this app makes: no browser addresses
    // the Go API directly (ADR 0008). The token goes in the BODY and not the
    // query string, so it reaches no access log on the way.
    const envelope = await callBackend<AnswerLinkView>("/api/v1/public/answer-link", {
      method: "POST",
      body: JSON.stringify({ token: signedToken }),
    });
    if (!envelope.data) {
      throw new Error("the API opened the link and answered with nothing");
    }
    view = envelope.data;
  } catch (error) {
    const failure = answerLinkFailure(error instanceof APIError ? error.code : undefined);
    return (
      <AnswerShell title={t("title")} description={t("description")}>
        <Alert variant="destructive">
          <AlertTitle>{t(`${failure}Title`)}</AlertTitle>
          <AlertDescription>{t(`${failure}Description`)}</AlertDescription>
        </Alert>
      </AnswerShell>
    );
  }

  return (
    <AnswerShell
      // The Event and the Ticket Type ARE the heading, because they are the only
      // two things the reader has to place this by. Drawn as coined: an
      // Organization's Event name is its own words in every language (ADR 0027).
      title={view.event_name}
      description={view.ticket_type_name}
    >
      <AnswerLinkForm token={signedToken} view={view} />
    </AnswerShell>
  );
}

function AnswerShell({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    // NO customerNav. The header's sign-in affordance is deliberately left off
    // this page: holding this link makes nobody a Customer, and an invitation to
    // sign in would suggest the two are connected when the whole design says
    // they are not.
    <StorefrontShell>
      <div className="mx-auto w-full max-w-xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={title} description={description} />
        {children}
      </div>
    </StorefrontShell>
  );
}
