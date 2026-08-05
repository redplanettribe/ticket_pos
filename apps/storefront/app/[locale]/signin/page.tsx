import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";

import { StorefrontShell } from "@/components/storefront-shell";
import { redirect } from "@/i18n/navigation";
import { localeAlternates } from "@/lib/alternates";
import { BRAND_NAME } from "@/lib/brand";
import { getCustomerSession } from "@/lib/customer-session";
import { safeNext } from "@/lib/destination";
import { googleSignInStartPath, isGoogleSignInConfigured } from "@/lib/google-signin";
import { toAppLocale } from "@/lib/locale";
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
  }>;
};

// safeNext now lives in lib/destination.ts, because the Google Sign-In callback
// applies the same guard to the destination it reads back out of its state
// cookie. One copy, one behaviour.

export default async function SignInPage({ params, searchParams }: SignInPageProps) {
  const { locale } = await params;
  // Every page declares its own locale; see the note in app/[locale]/layout.tsx.
  setRequestLocale(locale);
  const { next, expired, link, google, email } = await searchParams;
  const destination = safeNext(next);

  // Already signed in: there is nothing to prove, so go where they were headed.
  //
  // A Confirmation Link session deliberately does not count. It proves possession
  // of an email, not ownership of the address, and it reaches one Ticket Sale —
  // so someone holding one who comes here is asking to widen, and bouncing them
  // back would make that impossible.
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
          googleSignInHref={isGoogleSignInConfigured() ? googleSignInStartPath(destination) : null}
        />
      </div>
    </StorefrontShell>
  );
}
