import { Button } from "@ticket-pos/ui";
import { Heart } from "lucide-react";
import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
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
    return (
      <div className="flex items-center gap-1">
        <FollowingLink />
        <SignInLink />
      </div>
    );
  }

  return (
    <div className="flex items-center gap-1">
      <FollowingLink />
      <CustomerMenu
        email={session.data.email}
        firstName={session.data.first_name}
        lastName={session.data.last_name}
        avatarUrl={session.data.avatar_url}
      />
    </div>
  );
}

/**
 * The shortcut into the Following list, sitting immediately before the Avatar
 * chip (#230, parent #229).
 *
 * IT EXISTS BECAUSE THE MENU HID IT. Following was reachable only from behind
 * an unlabelled Avatar chip, which is a fine index of the Customer Area and a
 * poor way to teach a Customer that Follows exist at all. The menu keeps its
 * own entry — this is a shortcut, not a move, and taking the entry away would
 * break a path Customers already use. Only Following is promoted: putting My
 * Tickets up here beside it would dilute the one thing this change is for.
 *
 * DRAWN FOR EVERYBODY, signed in or not, for the same reason the Follow control
 * is (components/follow-button.tsx): a visitor who has never followed anything
 * is exactly the visitor this link is meant to reach, and they are anonymous.
 * Signed out it needs no plumbing of its own — the Following page already turns
 * a signed-out arrival into sign-in carrying a return to itself, because it must
 * survive one anyway, so this stays a plain address and the page keeps being the
 * single place that decides what a signed-out arrival means.
 *
 * The `Link` is the locale-aware one from @/i18n/navigation: this sits in chrome
 * on every Storefront page, and a bare anchor would drop a Customer reading
 * Spanish into English on the one press that is supposed to be a shortcut.
 *
 * The same heart as the Follow control, so the header and the button visibly
 * mean the same thing. Below `sm` the word goes and the glyph stays: the header
 * is a single row that also carries the mark or the Organization's name, and the
 * word is the part that can be spared.
 *
 * The name survives the word going, on `aria-label` and not on a `sr-only`
 * twin. An unlabelled heart reads as "favourite" — the shortlist a Follow is
 * deliberately not (CONTEXT.md) — so a reader who cannot see the glyph must
 * still be told, at every width; and a hidden twin is still a flex child, so the
 * button's `gap-2` would push the glyph off centre on exactly the narrow screens
 * this is for. The label carries the same string the visible word does, so the
 * two can never read differently.
 *
 * The word comes from the Following page's own key, not one of its own, so the
 * link and the heading it lands on cannot come to disagree.
 */
async function FollowingLink() {
  const t = await getTranslations("following");
  const label = t("title");

  return (
    <Button asChild variant="ghost" size="sm">
      <Link href="/following" aria-label={label}>
        <Heart aria-hidden="true" />
        <span className="hidden sm:inline">{label}</span>
      </Link>
    </Button>
  );
}
