import type { Metadata } from "next";
import { getMessages, getTranslations, setRequestLocale } from "next-intl/server";

import { Alert, AlertDescription, AlertTitle, PageHeader } from "@ticket-pos/ui";

import { ConsentControl } from "@/components/consent-control";
import { HeaderCustomerNav } from "@/components/header-customer-nav";
import { StorefrontShell } from "@/components/storefront-shell";
import { Link, redirect } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import { BRAND_NAME } from "@/lib/brand";
import { customerSessionToken, getPrivacy } from "@/lib/customer-session";
import { intlLocale, toAppLocale } from "@/lib/locale";
import { PRIVACY_POLICY_PATH } from "@/lib/privacy-policy";

// Same posture as every other Customer Area page: rendered per request with
// uncached reads. It matters more here than elsewhere — a cached page about
// somebody's consents would show one person another's answers.
export const dynamic = "force-dynamic";

type PrivacyPageProps = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: PrivacyPageProps): Promise<Metadata> {
  const { locale } = await params;
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "privacySettings" });
  return {
    title: t("metaTitle", { brand: BRAND_NAME }),
    // Private, in both languages, exactly as the rest of the Customer Area is.
    // The public Privacy Policy at /privacy-policy is the indexable one; this
    // page is one person's own answers and belongs in no index.
    robots: { index: false, follow: false },
  };
}

/**
 * The Customer's Privacy page (#268, parent #265): what they have agreed to,
 * and the controls that change it.
 *
 * RENDERING THIS PAGE WRITES NOTHING, which is the property to protect when
 * anything here is edited. The read below is a GET through the API, and there
 * is no other call on the render path: a settings page that recorded a refusal
 * because somebody looked at it would convert "never asked" into "denied" for
 * every person who opened it out of curiosity and closed it again — an act they
 * never performed, written into an evidence log that exists to say what people
 * did. Only pressing a control writes anything, and every control is a client
 * component posting to a BFF route.
 *
 * IT IS A SETTINGS SURFACE AND NOT A CAPTURE SURFACE. Showing the current state
 * of a consent does not breach the never-pre-tick rule: that rule governs the
 * moments where the platform ASKS — sign-in, checkout — and there a stored
 * answer decides only whether to show a box, never how to show it. This page
 * reports what is true. The line it must hold is that it is never the surface
 * that FIRST asks a Customer a question, which is why an unanswered consent
 * here offers a way to turn it on and no way to record a refusal.
 *
 * THE MARKETING CONTROL AND THE `/following` DIGEST TOGGLE ARE ONE SWITCH
 * RENDERED TWICE. They are the same column, moved by the same write path
 * (ADR 0034), so neither page synchronises anything with the other and neither
 * may grow a rule the other does not have. The toggle stays where it is: it is
 * beside the Follows because it explains what that list means, and this control
 * is here because a Customer looking for their privacy settings should not have
 * to know that marketing lives on a page about follows.
 *
 * Policy Acceptance is read-only and has no control anywhere, because it is not
 * withdrawable: it is absent from counsel's withdrawal form, it gates the
 * platform on a basis other than consent, and clearing it would re-gate the
 * person rather than free them (ADR 0038). The page says so rather than leaving
 * a reader to wonder why one row has no button.
 */
export default async function PrivacyPage({ params }: PrivacyPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);

  const privacy = await getPrivacy();

  if (privacy.status === "signed-out") {
    // Somebody's consents are theirs, so there is nothing to show a visitor who
    // is not signed in. The `expired` flag separates "your session ended" from
    // "you were never signed in" so the sign-in page can explain which, and
    // `next` brings them back here rather than to the explorer.
    const hadSession = Boolean(await customerSessionToken());
    return redirect({
      href: hadSession ? "/signin?expired=1&next=/privacy" : "/signin?next=/privacy",
      locale,
    });
  }

  const t = await getTranslations("privacySettings");
  // What the read failed with, in this page's language: chosen by the API's own
  // error code, falling back to the API's message for a code this catalog has
  // never heard of (ADR 0023), and to this page's own sentence when the API was
  // never reached and so said nothing at all.
  const loadFailure =
    privacy.status === "error"
      ? (apiErrorMessage((await getMessages()).errors, privacy) ?? t("loadNetworkFailed"))
      : null;

  return (
    <StorefrontShell customerNav={<HeaderCustomerNav />}>
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10 sm:py-12">
        <PageHeader title={t("title")} description={t("description")} />

        {privacy.status === "error" ? (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            {/* Which failure it was stays the API's to say; only the words are
                this page's. Nothing is drawn beneath it: a page that guessed at
                somebody's consents while the read was failing would be showing
                them answers the platform could not confirm they gave. */}
            <AlertDescription>{loadFailure}</AlertDescription>
          </Alert>
        ) : (
          <>
            <section className="space-y-2 rounded-lg border p-4">
              <h2 className="font-medium">{t("policyHeading")}</h2>
              <p className="text-muted-foreground text-sm">
                {privacy.data.policy_version && privacy.data.policy_accepted_at
                  ? t("policyAccepted", {
                      // The edition THIS Customer accepted, which is not
                      // necessarily the one in effect — naming the current one
                      // would tell them they had agreed to a text nobody has
                      // shown them.
                      version: privacy.data.policy_version,
                      date: acceptedOn(privacy.data.policy_accepted_at, locale),
                    })
                  : t("policyNone")}
              </p>
              <p className="text-muted-foreground text-sm">{t("policyNotWithdrawable")}</p>
              <Link href={PRIVACY_POLICY_PATH} className="text-sm underline underline-offset-4">
                {t("policyLink")}
              </Link>
            </section>

            <section className="space-y-4">
              <h2 className="font-medium">{t("consentsHeading")}</h2>
              {/* Two controls, each moving ONE consent. There is deliberately no
                  single action here that answers both at once: withdrawing
                  everything is its own act, behind a dialog that says what it
                  does and does not mean, and it lands in #269. */}
              <ConsentControl
                purpose="marketing"
                state={privacy.data.consents.marketing_consent}
                title={t("marketingTitle")}
                description={t("marketingDescription")}
              />
              <ConsentControl
                purpose="networking"
                state={privacy.data.consents.networking_consent}
                title={t("networkingTitle")}
                description={t("networkingDescription")}
              />
            </section>

            {/* What is true of this whole page, and it must stay true: it never
                says processing stops. This platform keeps performing
                contract-based processing for anybody holding a Ticket, so copy
                promising otherwise would be untrue — "passive" is scoped to
                consent-based processing alone (CONTEXT.md). It also says this is
                not deletion, because a reader who assumed it was would think
                they had asked for something they have not. */}
            <p className="border-t pt-6 text-sm text-muted-foreground">{t("footnote")}</p>
          </>
        )}
      </div>
    </StorefrontShell>
  );
}

/**
 * When the acceptance was recorded, in the reader's language.
 *
 * The day alone, with no time: what a Customer needs to recognise is which
 * occasion this was, and a timestamp to the second invites them to read
 * precision into a fact that is only ever "the day I signed in". Language and
 * time zone are separate concerns everywhere in this app (lib/format.ts); this
 * one is drawn in the reader's own zone, because unlike an Event's start it is
 * a moment in their life rather than in a venue's calendar.
 */
function acceptedOn(acceptedAt: string, locale: string): string {
  const date = new Date(acceptedAt);
  if (Number.isNaN(date.getTime())) return acceptedAt;
  return new Intl.DateTimeFormat(intlLocale(toAppLocale(locale)), {
    dateStyle: "long",
  }).format(date);
}
