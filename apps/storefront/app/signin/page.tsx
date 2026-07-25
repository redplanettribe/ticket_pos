import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { StorefrontShell } from "@ticket-pos/ui";

import { getCustomerSession } from "@/lib/customer-session";
import { safeNext } from "@/lib/destination";
import { googleSignInStartPath, isGoogleSignInConfigured } from "@/lib/google-signin";

import { SignInForm } from "./signin-form";

// Same posture as every other Storefront page: rendered per request, nothing
// cached. Reading the session cookie therefore costs nothing extra here.
export const dynamic = "force-dynamic";

// "/signin" and "/tickets" are static segments and therefore shadow the
// "/{orgSlug}" Organization page for those two slugs, exactly as the existing
// "/api" route folder already does. Nothing reserves slugs server-side today;
// noted here so it is a known trade rather than a surprise.

export const metadata: Metadata = {
  title: "Sign in · Multiticketing",
  description: "Sign in with your email to see your tickets.",
};

type SignInPageProps = {
  searchParams: Promise<{ next?: string; expired?: string; link?: string; google?: string }>;
};

// safeNext now lives in lib/destination.ts, because the Google Sign-In callback
// applies the same guard to the destination it reads back out of its state
// cookie. One copy, one behaviour.

export default async function SignInPage({ searchParams }: SignInPageProps) {
  const { next, expired, link, google } = await searchParams;
  const destination = safeNext(next);

  // Already signed in: there is nothing to prove, so go where they were headed.
  //
  // A Confirmation Link session deliberately does not count. It proves possession
  // of an email, not ownership of the address, and it reaches one Ticket Sale —
  // so someone holding one who comes here is asking to widen, and bouncing them
  // back would make that impossible.
  const session = await getCustomerSession();
  if (session.status === "ok" && session.data.ticket_sale_id === null) {
    redirect(destination);
  }

  return (
    <StorefrontShell>
      <div className="mx-auto flex w-full max-w-md flex-col justify-center px-4 py-12 sm:py-16">
        <SignInForm
          next={destination}
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
