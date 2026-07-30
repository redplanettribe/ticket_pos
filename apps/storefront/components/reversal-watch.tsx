"use client";

import { toast } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useRef, useTransition } from "react";

import type { ReversalRequestStatus } from "@/lib/customer-session";

/**
 * Roughly every three seconds, for roughly a minute (#160).
 *
 * The interval is what the Customer perceives as "it happened while I watched";
 * the budget is what stops a page somebody walked away from asking the API
 * forever. Sixty seconds is chosen against the Reversal Reconciler's first
 * backoff step of ten seconds (ADR 0024): a request that resolves at all
 * usually resolves within the first few probes, so the poll covers the common
 * case. Nothing breaks when it gives up — the Reconciler goes on pursuing the
 * request whether or not anybody is watching, and the "Refund in progress"
 * state stays on screen and stays true until a later visit reads the outcome
 * off the card.
 */
const POLL_INTERVAL_MS = 3_000;
const POLL_BUDGET_MS = 60_000;

/**
 * How many ticks a failed Customer Area read is worth before the page stops
 * asking (#160).
 *
 * Small on purpose: this is a tolerance for a blip on one of the ~20 reads a
 * single watch makes, not a retry loop for an API that is down.
 */
const RETRY_ATTEMPTS = 3;

/**
 * Watches one Ticket Sale's in-flight Reversal Request resolve while the
 * Customer is still looking at the page (#160).
 *
 * It renders nothing. Both of its jobs are things a server-rendered card cannot
 * do for itself: re-asking for the page while an answer is outstanding, and
 * saying out loud what the answer turned out to be.
 *
 * The re-ask is `router.refresh()` — the Customer Area read this page is
 * already built from, run again. That read is a GET, and asking for it is what
 * drains this Customer's own stuck request server-side; the poll never posts a
 * reversal, so no amount of polling can start a second one or move any money
 * that the Customer did not already ask to have moved.
 *
 * It is mounted for every sale on the full-session surface rather than only for
 * pending ones, because the transition out of pending is the thing it reports:
 * a component that unmounted the moment the request resolved would take the
 * news with it.
 */
export function ReversalWatch({
  pending,
  reversed,
  reversalStatus,
}: {
  /** Whether a Reversal Request for this sale is in flight right now. */
  pending: boolean;
  /** Whether the sale is reversed — the resolution the Customer may be told about. */
  reversed: boolean;
  /**
   * The Reversal Request's own state, which is the only thing that separates the
   * two ways of not being reversed: a refusal and an Unresolved Reversal both
   * leave the sale active and nothing in flight, and only one of them is news.
   */
  reversalStatus: ReversalRequestStatus | null;
}) {
  const router = useRouter();
  const t = useTranslations("customerArea");
  // `router.refresh()` returns nothing, so the transition is how the refetch is
  // observed at all: `refreshing` stays true until the new server render has
  // landed. Read through a ref because the interval closes over its own tick.
  const [refreshing, startTransition] = useTransition();
  const refreshingRef = useRef(false);
  useEffect(() => {
    refreshingRef.current = refreshing;
  }, [refreshing]);

  useEffect(() => {
    if (!pending) return;
    // Taken once, when polling starts, so the budget measures the wait rather
    // than the gap since the last tick: `router.refresh()` re-renders this
    // component without re-running the effect, which is exactly what keeps a
    // request that stays pending from renewing its own minute.
    const deadline = Date.now() + POLL_BUDGET_MS;
    const timer = setInterval(() => {
      if (Date.now() >= deadline) {
        clearInterval(timer);
        return;
      }
      // A tick that arrives while the last read is still outstanding is dropped
      // rather than queued. Refreshes do not coalesce, so a Customer Area read
      // slower than the interval would otherwise pile up a backlog that keeps
      // refetching after the budget is spent and after this effect has been
      // torn down. Skipping degrades the poll to "as often as the API answers",
      // which is the fastest it could honestly be anyway.
      if (refreshingRef.current) return;
      startTransition(() => router.refresh());
    }, POLL_INTERVAL_MS);
    // Two of the acceptance criteria are this one line. Navigating away
    // unmounts the card and clears the timer; a resolution flips `pending`
    // false, which re-runs the effect and clears it on the way in — so polling
    // stops on the same render that shows the outcome, without anything having
    // to notice twice.
    return () => clearInterval(timer);
  }, [pending, router]);

  // What the previous server render said, so the resolution can be spotted as a
  // change rather than as a state. Seeded from the first render on purpose: a
  // Customer who loads the page onto an already-reversed sale is reading
  // history, not watching something happen, and must not be toasted about it.
  const wasPending = useRef(pending);
  useEffect(() => {
    const resolved = wasPending.current && !pending;
    wasPending.current = pending;
    if (!resolved) return;
    if (reversed) {
      // Deliberately the same toast the dialog shows when the provider answers
      // in time (components/undo-purchase.tsx). The Customer asked one question
      // and got one answer; that it took a minute and arrived by a poll is the
      // platform's problem, not a different outcome to word differently.
      toast.success(t("undoneToast"));
      return;
    }
    if (reversalStatus === "refused") {
      // The refusal ADR 0024 accepts we will have to deliver late: we said we
      // were processing a refund and it is not coming. The sentence leads with
      // the tickets because that is the actionable half — they were never
      // released, they are still good for entry, and somebody who has since made
      // other plans needs to know that before they act on the bad news.
      //
      // Keyed on this state and not on "resolved without being reversed",
      // because that sentence is a claim about the money and only a refusal
      // supports it: a refusal is the one ending where the platform knows
      // nothing moved.
      toast(t("refundRefusedToast"));
      return;
    }
    // Everything else that stops being pending is an Unresolved Reversal, and the
    // Customer hears nothing — the same silence the backend keeps by sending no
    // email. There is nothing true to say: the platform stopped asking without
    // learning whether the money went back, and where it gave up on committing a
    // reversal the provider had agreed to, it went back already. A sentence
    // covering both would have to be wrong about one.
    //
    // So the card simply goes back to looking untouched, which is what the sale
    // is, and a Customer who presses Undo again is answered by name
    // (REVERSAL_UNRESOLVED) rather than guessed at here.
  }, [pending, reversed, reversalStatus, t]);

  return null;
}

/**
 * Asks the Customer Area for itself a few more times after a read failed
 * (#160). It renders nothing and sits beside the failure the page already
 * shows; the page keeps saying what went wrong the whole time.
 *
 * It exists because the failure is destructive to the watch above: the page
 * draws the error in place of the cards, so one blip on one of the ~20 reads a
 * single watch makes unmounts it, and polling would end there — pending on
 * screen, nobody asking, and the resolution never reported. A handful of
 * retries buys the watch back across a blip without pretending a sustained
 * outage is temporary; if they are all spent the reader is left with an error
 * that names what happened and a page they can reload, which is the outcome a
 * failed read has always had here.
 *
 * A recovered read re-mounts the watch fresh, so a resolution that landed
 * during the gap arrives as the resolved card rather than as a toast. That is
 * the point of the seed in `wasPending`: the Customer is reading what happened,
 * not watching it happen.
 *
 * It cannot know whether a reversal was pending — the read that would have said
 * so is the one that failed — so it retries every failure of this page, which
 * is why the count is small.
 */
export function RetryFailedRead() {
  const router = useRouter();
  const [refreshing, startTransition] = useTransition();
  const refreshingRef = useRef(false);
  useEffect(() => {
    refreshingRef.current = refreshing;
  }, [refreshing]);

  useEffect(() => {
    let left = RETRY_ATTEMPTS;
    const timer = setInterval(() => {
      // Same guard, same reason as the poll: an attempt is only spent on a read
      // that actually went out, so a slow failure is waited for rather than
      // burning the budget on ticks that overlap it.
      if (refreshingRef.current) return;
      if (left <= 0) {
        clearInterval(timer);
        return;
      }
      left -= 1;
      startTransition(() => router.refresh());
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [router]);

  return null;
}
