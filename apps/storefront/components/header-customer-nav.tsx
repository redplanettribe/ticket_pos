import Link from "next/link";

import { Button } from "@ticket-pos/ui";

import { getCustomerSession } from "@/lib/customer-session";

import { SignOutButton } from "./sign-out-button";

/**
 * Sign-in state in the Storefront header: a way in when signed out, and which
 * email you are signed in as plus a way out when signed in.
 *
 * An anonymous visitor pays nothing for this. With no Customer Session cookie
 * present, getCustomerSession returns immediately without calling the API, so
 * browsing Events, Storefront listings, and the global explorer costs exactly
 * what it did before this existed.
 */
export async function HeaderCustomerNav() {
  const session = await getCustomerSession();

  if (session.status !== "ok") {
    return (
      <Button asChild variant="ghost" size="sm">
        <Link href="/signin">Sign in</Link>
      </Button>
    );
  }

  return (
    <div className="flex items-center gap-1 sm:gap-2">
      {/* Which identity you are using, never ambiguous. Truncated on narrow
          screens, where the full address is still available as a tooltip. */}
      <span
        className="hidden max-w-[12rem] truncate text-sm text-muted-foreground sm:inline"
        title={session.data.email}
      >
        {session.data.email}
      </span>
      <Button asChild variant="ghost" size="sm">
        <Link href="/tickets">Your tickets</Link>
      </Button>
      <SignOutButton />
    </div>
  );
}
