import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { StorefrontShell } from "@ticket-pos/ui";

import { getCustomerSession } from "@/lib/customer-session";

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
  searchParams: Promise<{ next?: string; expired?: string; link?: string }>;
};

/**
 * safeNext keeps the post-sign-in destination inside this Storefront. Anything
 * that is not a plain absolute path — a full URL, or a protocol-relative "//host"
 * — is discarded, so the sign-in page can never be used to bounce a visitor to
 * another site.
 */
function safeNext(next: string | undefined): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) {
    return "/tickets";
  }
  return next;
}

export default async function SignInPage({ searchParams }: SignInPageProps) {
  const { next, expired, link } = await searchParams;
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
        />
      </div>
    </StorefrontShell>
  );
}
