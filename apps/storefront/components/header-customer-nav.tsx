import { getCustomerSession } from "@/lib/customer-session";

import { CustomerMenu } from "./customer-menu";
import { SignInLink } from "./sign-in-link";

/**
 * Sign-in state in the Storefront header: a way in when signed out, and the
 * Avatar chip with its account menu when signed in (email, Customer Area,
 * sign out — see CustomerMenu).
 *
 * An anonymous visitor pays nothing for this. With no Customer Session cookie
 * present, getCustomerSession returns immediately without calling the API, so
 * browsing Events, Storefront listings, and the global explorer costs exactly
 * what it did before this existed.
 */
export async function HeaderCustomerNav() {
  const session = await getCustomerSession();

  if (session.status !== "ok") {
    return <SignInLink />;
  }

  return (
    <CustomerMenu
      email={session.data.email}
      firstName={session.data.first_name}
      lastName={session.data.last_name}
      avatarUrl={session.data.avatar_url}
    />
  );
}
