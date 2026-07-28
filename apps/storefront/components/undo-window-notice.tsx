import Link from "next/link";
import type { ReactNode } from "react";

import { Button } from "@ticket-pos/ui";

import { signInToUndoHref } from "@/lib/undo-window";

/**
 * "You can undo this purchase until …", wherever it is said.
 *
 * Three surfaces say it and must say it identically: the Customer Area's card,
 * where the undo itself sits; the checkout success page a guest lands on seconds
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
 * undoes, and the guest surfaces hand it a link to sign in. Neither this
 * component nor anything under it on a guest surface can execute a reversal.
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
  return (
    <div
      className={`flex flex-wrap items-center justify-between gap-x-4 gap-y-3 text-left ${className}`}
    >
      <p className="text-sm text-muted-foreground">
        You can undo this purchase until{" "}
        <span className="font-medium text-foreground">{deadline}</span> Ecuador time.
      </p>
      {children}
    </div>
  );
}

/**
 * The guest surfaces' action: the one click that turns the Customer Session
 * requirement from a dead end into a door.
 *
 * Undoing a purchase requires a Customer Session and always will — a
 * Confirmation Link travels by email and gets forwarded, so it must never carry
 * a money-moving action (ADR 0018). But online checkout is guest-facing, which
 * leaves the buyer most likely to want an undo holding no session at all. A
 * guest who is not told this will email the Organization instead, which is the
 * outcome self-service was meant to remove.
 *
 * Someone who already holds a full Customer Session is not asked to prove
 * anything again: they are sent straight to the Customer Area, where their sale
 * and its undo button are.
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
}: {
  /** True when the visitor already holds a full Customer Session. */
  signedIn: boolean;
  /** The address the purchase was made under, to prefill sign-in with. */
  email: string | null;
}) {
  if (signedIn) {
    return (
      <Button asChild variant="outline" className="h-11">
        <Link href="/tickets">Undo it in your tickets</Link>
      </Button>
    );
  }
  return (
    <Button asChild variant="outline" className="h-11">
      <Link href={signInToUndoHref(email)}>Sign in to undo</Link>
    </Button>
  );
}
