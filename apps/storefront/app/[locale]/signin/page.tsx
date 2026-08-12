import type { Metadata } from "next";
import { cookies } from "next/headers";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { StorefrontShell } from "@/components/storefront-shell";
import { redirect } from "@/i18n/navigation";
import { localeAlternates } from "@/lib/alternates";
import { getPrivacyPolicy } from "@/lib/api";
import { BRAND_NAME } from "@/lib/brand";
import { getCustomerSession } from "@/lib/customer-session";
import { safeNext } from "@/lib/destination";
import { safeFollowIntent } from "@/lib/follow-intent";
import { googleSignInStartPath, isGoogleSignInConfigured } from "@/lib/google-signin";
import { toAppLocale } from "@/lib/locale";
import { PENDING_CONSENT_COOKIE, decodePendingConsent } from "@/lib/pending-consent";
import { storefrontBaseUrl } from "@/lib/site";
import { safePrefillEmail } from "@/lib/signin-prefill";

import { SignInForm } from "./signin-form";

// Same posture as every other Storefront page: rendered per request, nothing
// cached. Reading the session cookie therefore costs nothing extra here.
export const dynamic = "force-dynamic";

// "/signin" and "/tickets" are static segments and therefore shadow the
// "/{locale}/{orgSlug}" Organization page for those two slugs, exactly as the
// existing "/api" route folder already does — as do the locale tokens
// themselves, one segment up. Nothing reserves slugs server-side today; noted
// here so it is a known trade rather than a surprise.

/**
 * Sign-in is annotated like the public pages, unlike the Customer Area it leads
 * to. It declares no `robots`, so it is an indexable page in two languages —
 * leaving it unpaired would be the worst of both: still indexed, but with a
 * Spanish and an English version competing as unrelated duplicates. Should this
 * page ever be marked noindex, the annotation should go with it.
 *
 * `?next=`, `?expired=` and the rest never reach the canonical: they steer one
 * visit, they do not make a different page, and each one would otherwise mint
 * an indexable address of its own.
 */
export async function generateMetadata({ params }: SignInPageProps): Promise<Metadata> {
  const { locale } = await params;
  const { canonical, languages } = localeAlternates(
    "/signin",
    toAppLocale(locale),
    storefrontBaseUrl(),
  );
  // Metadata renders before the page declares its locale, so the namespace is
  // asked for the locale off the URL explicitly rather than for the request's.
  const t = await getTranslations({ locale, namespace: "signin" });
  return {
    // The brand is interpolated rather than written into the catalog, so a
    // translator has a sentence to translate and not a name to render.
    title: t("metaTitle", { brand: BRAND_NAME }),
    description: t("metaDescription"),
    alternates: { canonical, languages },
  };
}

type SignInPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{
    next?: string;
    expired?: string;
    link?: string;
    google?: string;
    /**
     * The address to start the form with, sent by the guest surfaces that offer
     * "Sign in to undo" (#121). It is a convenience and never an assertion: the
     * passcode still has to be proved, so a prefilled field grants nothing.
     */
    email?: string;
    /**
     * The Follow somebody pressed before they could be asked who they are
     * (#219): "organization:<slug>". It travels here in the open, as an explicit
     * parameter rather than in browser storage, so that it is server-visible and
     * can be validated — by this page before it is shown to anybody, and again
     * by the API before it is acted on.
     *
     * It is not a destination and cannot become one: where the visitor lands is
     * `next` above and nothing else. And it names a subject, never a subscriber
     * — the `email` beside it prefills a field and proves nothing, while whose
     * Follow this becomes is decided by the session verification mints.
     */
    follow?: string;
    /**
     * "pending" when the visitor was just sent here by a Google Sign-In that
     * the consent gate held (#252).
     *
     * A MARKER, never the credential. It says only that this page should look
     * for a held sign-in; the pending-consent token itself arrives in an
     * httpOnly cookie, because a credential in an address ends up in history,
     * in logs and in the `Referer` of the Privacy Policy link the step offers.
     * Typing this parameter by hand gets the ordinary email step.
     */
    consent?: string;
  }>;
};

// safeNext now lives in lib/destination.ts, because the Google Sign-In callback
// applies the same guard to the destination it reads back out of its state
// cookie. One copy, one behaviour.

export default async function SignInPage({ params, searchParams }: SignInPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { next, expired, link, google, email, follow, consent } = await searchParams;
  const destination = safeNext(next);
  // The held Google Sign-In, if this visitor is coming back from one (#252).
  //
  // The cookie is read ONLY when the address says to look for it. That is what
  // keeps a cookie that outlives its step from ambushing an ordinary visit to
  // /signin with a consent form: a fresh visit carries no marker and sees the
  // email step, whatever is still in the jar.
  //
  // It is read here rather than fetched by the form because this page is the
  // only thing that can read it — the cookie is httpOnly, which is the point of
  // it (ADR 0008, ADR 0010). What the form receives is the same
  // consent-required outcome the passcode door hands it out of a fetch
  // response; where it came from is this page's problem and not the step's.
  const pendingConsent =
    consent === "pending"
      ? decodePendingConsent((await cookies()).get(PENDING_CONSENT_COOKIE)?.value)
      : null;
  // Guarded before it reaches the form or the Google button, and dropped rather
  // than refused when it is not an intent: a junk `follow` on an address anybody
  // can craft must never be the reason somebody cannot sign in. They get the
  // ordinary form and land where `next` says, having followed nothing.
  const intent = safeFollowIntent(follow);

  // Already signed in: there is nothing to prove, so go where they were headed.
  //
  // A Confirmation Link session deliberately does not count. It proves possession
  // of an email, not ownership of the address, and it reaches one Ticket Sale —
  // so someone holding one who comes here is asking to widen, and bouncing them
  // back would make that impossible.
  //
  // A visitor who arrives already signed in carrying an intent is sent on
  // without it being acted on, and that is the honest behaviour rather than a
  // gap worth code: the control they pressed is only a link to here when the
  // server rendered it signed out, so reaching this branch means a session
  // appeared in another tab in between. They land on the page they came from
  // with the control drawn as the write it is for a signed-in Customer, one
  // press away — and nothing here quietly writes a Follow on the strength of a
  // URL parameter and a session it did not just mint.
  const session = await getCustomerSession();
  if (session.status === "ok" && session.data.ticket_sale_id === null) {
    // The locale-aware redirect: `destination` is a locale-free path — it comes
    // from `?next=`, which the header's sign-in link writes without a prefix —
    // and gains the language this page is being read in.
    return redirect({ href: destination, locale });
  }

  return (
    <StorefrontShell>
      <div className="mx-auto flex w-full max-w-md flex-col justify-center px-4 py-12 sm:py-16">
        <SignInForm
          next={destination}
          // Guarded before it reaches an input: anything not plausibly an email
          // is dropped, because a page anybody can link to must not be able to
          // put arbitrary text in a field that looks like this app's own
          // knowledge of the visitor.
          initialEmail={safePrefillEmail(email)}
          expired={expired === "1"}
          linkFailure={link === "expired" || link === "invalid" ? link : null}
          googleFailed={google === "failed"}
          // Absent credentials the button is not rendered at all, so a developer
          // running `make dev` without a Google client sees the passcode form
          // and nothing broken (PRD "Local development").
          // The intent rides both doors. They are equal Proof of Email Ownership
          // (ADR 0011), so a visitor who pressed Follow and then chose Google
          // must not silently lose it — it goes into the state cookie the start
          // route mints and comes back out at the callback.
          followIntent={intent}
          // The consent step a Google Sign-In was held at, or null (#252). The
          // form starts ON that step when it is set, which is what makes the two
          // doors reach the same place: from here on the Google visitor is in
          // the identical component state a passcode visitor's verify put them
          // in, and finishes through the identical submission.
          pendingConsent={pendingConsent}
          // The Short Notice and the three checkbox labels, fetched from the
          // SAME public endpoint the Privacy Policy page renders (#250, ADR
          // 0036). One read, one edition: the page a visitor follows the link to
          // and the notice they accept beside the boxes cannot come from
          // different editions, because they are literally the same payload.
          //
          // Fetched on every render of this page rather than only when a consent
          // step turns up, because whether one will is not knowable here — it is
          // disclosed only after a passcode is proved, and asking earlier would
          // be asking the API about an address nobody has proven (ADR 0035).
          //
          // Null when the API cannot be reached, which the form renders as a
          // consent step it cannot complete rather than as boxes with no notice
          // beside them: consent to text nobody was shown is not consent.
          policy={await getPrivacyPolicy(locale)}
          googleSignInHref={
            isGoogleSignInConfigured() ? googleSignInStartPath(destination, intent) : null
          }
        />
      </div>
    </StorefrontShell>
  );
}
