import { useTranslations } from "next-intl";
import type { ReactNode } from "react";

import { Button } from "@ticket-pos/ui";

import { Link } from "@/i18n/navigation";
import { customerAreaSaleHref } from "@/lib/destination";
import { signInToUndoHref } from "@/lib/undo-window";

/**
 * "You can undo this purchase until …", wherever it is said.
 *
 * Three surfaces say it and must say it identically: the Customer Area's card,
 * where the undo itself sits; the checkout success page a buyer lands on seconds
 * after paying; and the Confirmation Link page they return to when they
 * reconsider (#121). One component means the deadline sentence, the Ecuador-time
 * qualifier and the word "undo" cannot drift apart across them.
 *
 * The deadline is named in Ecuador time and says so. It is the platform's own
 * wall clock and not the Event's (ADR 0018), and a buyer whose show is abroad
 * would otherwise have no way to tell which 8:00 PM was meant.
 *
 * The word is "undo". Not "cancel" — that belongs to an Event being called off —
 * and not "refund", which is wrong for a free Online Sale where nothing was ever
 * collected.
 *
 * The action is a child rather than a prop of this component, because it is the
 * one thing that genuinely differs: the Customer Area hands it the button that
 * undoes, and the sessionless surfaces hand it a link to sign in. Neither this
 * component nor anything under it on one of those can execute a reversal.
 *
 * The words live in the `checkout` namespace for the same reason they live in
 * one component: the buyer meets this sentence for the first time seconds after
 * paying, and a Customer Area namespace that could word it differently would
 * reintroduce exactly the drift this component exists to prevent.
 */
export function UndoWindowNotice({
  deadline,
  className = "mt-4 border-t pt-4",
  children,
}: {
  /** The already-formatted deadline; callers get it from lib/undo-window. */
  deadline: string;
  /**
   * How the block sits in its page. The wording never varies; only its framing
   * does — a divider inside a Customer Area card, a bordered panel on the
   * checkout success page.
   */
  className?: string;
  /** The action offered beside it, if any. */
  children?: ReactNode;
}) {
  const t = useTranslations("checkout");

  return (
    <div
      className={`flex flex-wrap items-center justify-between gap-x-4 gap-y-3 text-left ${className}`}
    >
      <p className="text-sm text-muted-foreground">
        {/* The emphasis is inside the sentence, so the message carries it as a
            tag: a language that puts the deadline elsewhere must be able to. */}
        {t.rich("undoUntil", {
          deadline,
          when: (chunks) => <span className="font-medium text-foreground">{chunks}</span>,
        })}
      </p>
      {children}
    </div>
  );
}

/**
 * The sessionless surfaces' action: the one click that turns the Customer Session
 * requirement from a dead end into a door.
 *
 * Undoing a purchase requires a Customer Session and always will — a
 * Confirmation Link travels by email and gets forwarded, so it must never carry
 * a money-moving action (ADR 0018). Two surfaces still meet a reader who holds
 * none: the Confirmation Link page, which is reached from an inbox and by
 * design proves nothing; and the checkout success page, where since ADR 0054 a
 * missing session means not a guest but a buyer whose own session did not
 * survive the round trip through the Payment Provider (#387). Somebody who is
 * not told this will email the Organization instead, which is the outcome
 * self-service was meant to remove.
 *
 * Someone who already holds a full Customer Session is not asked to prove
 * anything again: they are sent straight to the sale itself, where its undo
 * button is. Not to the Customer Area's list — a buyer with a season's worth of
 * tickets, reaching for the undo on the thing they bought ninety seconds ago,
 * would be handed a page to scan. The Customer Area has no per-sale route, so the
 * destination is that card's own anchor on it; someone signing in first carries
 * the same anchor through as their `next`.
 *
 * When this app does not know which sale it means — a buyer whose checkout
 * context cookie has expired — the destination is the list, exactly as it was
 * before. A less precise landing is the honest degradation; a link to an anchor
 * nothing renders would be a link that quietly does nothing.
 *
 * This is a link and nothing more. It performs no reversal, and it is not a
 * component that could grow one — the page it points at is where the action
 * lives, behind the session that authorises it.
 *
 * It is a full-height (h-11) target rather than a small one because one of the
 * two places it appears is the checkout success page, where every sibling CTA is
 * h-11 and the checkout flow's 44×44px minimum applies. The same size is drawn on
 * the Confirmation Link card: a buyer reaching for the only way back to their
 * money on a phone is the last person to give a 32px target.
 */
export function SignInToUndo({
  signedIn,
  email,
  ticketSaleId = null,
}: {
  /** True when the visitor already holds a full Customer Session. */
  signedIn: boolean;
  /** The address the purchase was made under, to prefill sign-in with. */
  email: string | null;
  /**
   * The purchase this notice is about, so the link lands on it rather than on
   * the whole Customer Area. Null when this app does not know — then the list.
   */
  ticketSaleId?: string | null;
}) {
  const t = useTranslations("checkout");

  if (signedIn) {
    return (
      <Button asChild variant="outline" className="h-11">
        <Link href={customerAreaSaleHref(ticketSaleId)}>{t("undoInTickets")}</Link>
      </Button>
    );
  }
  return (
    <Button asChild variant="outline" className="h-11">
      <Link href={signInToUndoHref(email, ticketSaleId)}>{t("signInToUndo")}</Link>
    </Button>
  );
}
