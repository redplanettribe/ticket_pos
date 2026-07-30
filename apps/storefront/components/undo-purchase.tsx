"use client";

import {
  Alert,
  AlertDescription,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  toast,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { apiErrorMessage } from "@/lib/api-errors";
import type { TicketSaleReversal } from "@/lib/customer-session";

/**
 * "Undo this purchase" on a Ticket Sale card in the Customer Area (#119,
 * ADR 0018).
 *
 * The word is "undo" throughout. Not "cancel", which in this system means an
 * Event being called off, and not "refund" — the platform's own word for what
 * happens to a Ticket Sale is a Sale Reversal, whether or not money moves.
 *
 * A purchase that cost something says so in the dialog (#120): the payment goes
 * back through the Payment Provider that took it, and a buyer about to release
 * their tickets deserves to be told that in the same breath. A free claim keeps
 * the shorter sentence, because there is nothing to return.
 *
 * It is drawn only where the API said `reversible`, and pressing it is still a
 * question the API answers fresh: the window is enforced server-side, so a card
 * rendered before the deadline and pressed after it comes back refused rather
 * than quietly succeeding. Which refusal it is stays the API's to decide; the
 * sentence is this app's, chosen by the API's code (ADR 0023).
 *
 * Pressing it has three outcomes rather than two (ADR 0024). It is done, it was
 * refused, or the Payment Provider went silent and a Reversal Request is in
 * flight — the last answered 202 with `status: "pending"`. The third is why this
 * dialog reads the body's own status instead of trusting the response having
 * been ok: a 202 is ok, and reporting it as done would tell a Customer their
 * money is back when the platform does not yet know that it is.
 *
 * The confirmation step is deliberate rather than ceremonial. This is the one
 * destructive action a Customer has, it cannot be taken back — capacity returns
 * to the Ticket Type and the tickets may be gone the moment they are released —
 * and the deadline means a mis-tap often cannot be recovered by buying again.
 */
type UndoPurchaseProps = {
  /** The Ticket Sale to undo; the only thing sent, and only ever the caller's own. */
  saleId: string;
  /** The Event's name, so the dialog names what is being given up. */
  eventName: string;
  /** The Sale Confirmation reference, the thing a Customer would quote to an organizer. */
  confirmationRef: string;
  /**
   * What the purchase cost, already formatted, or null when it cost nothing.
   * Present it and the dialog promises the payment back; absent, it stays quiet
   * rather than mentioning money that never changed hands.
   */
  paidLabel?: string | null;
};

/**
 * The BFF route's relay of the undo, typed by the union the API actually answers
 * with rather than by a local guess at it. `data` is null only on the error path,
 * which is read through `error` instead.
 *
 * The union is the point: `TicketSaleReversal` discriminates on `status`, so the
 * comparison the "done" toast rests on is checked against the values the API can
 * really send. A future state — or a rename of `reversed` — stops this file from
 * compiling instead of quietly leaving the success branch unreachable and every
 * outcome reported as pending.
 */
type Envelope = {
  data: TicketSaleReversal | null;
  error: { code: string; message: string } | null;
};

/**
 * Why the undo did not happen, as a fact rather than as a sentence — the same
 * shape the checkout dialog holds, and for the same reasons. `code` is what the
 * copy is chosen by, `message` is the API's own words for a code the catalog
 * does not know, and `fallback` is this app's sentence for the failure the API
 * never got to report.
 */
type UndoError = {
  code: string | null;
  message: string | null;
  fallback: "undoFailed" | "undoNetworkFailed";
};

export function UndoPurchase({
  saleId,
  eventName,
  confirmationRef,
  paidLabel = null,
}: UndoPurchaseProps) {
  const router = useRouter();
  const t = useTranslations("customerArea");
  // The close affordance belongs to the chrome the shell owns, so it reads from
  // the same namespace every other dismiss control does.
  const shell = useTranslations("shell");
  // Keys of API codes rather than message keys, so the catalog is read as plain
  // data rather than through `t`.
  const errorCopy = useMessages().errors;
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<UndoError | null>(null);

  async function handleUndo() {
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(
        `/api/customer/ticket-sales/${encodeURIComponent(saleId)}/reverse`,
        { method: "POST" },
      );
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        // The API owns every reason this can fail — the window has closed, the
        // sale is already undone, the payment cannot be reversed — and its code
        // is which of them happened. Deciding that again here would only let the
        // two disagree; choosing the words for it does not (ADR 0023).
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "undoFailed",
        });
        return;
      }
      setOpen(false);
      // Only `reversed` is done, and the test is positive on purpose: anything
      // else — including a body this build does not recognise — falls to "we are
      // processing it", which is at worst premature, where the other default
      // would tell somebody their money is back on no evidence at all.
      if (envelope.data?.status === "reversed") {
        toast.success(t("undoneToast"));
      } else {
        // Not toast.success: nothing has succeeded. This is the one place the
        // Customer Area says "refund" rather than "undo", and it is earned —
        // a Reversal Request exists only where a Payment Provider took real
        // money and has not yet said what it did with it. A free claim reverses
        // synchronously and never reaches this branch.
        toast(t("undoPendingToast"));
      }
      // The card, the badge and the ticket lists are all server-rendered from
      // the Customer Area read, so the page is asked for the new truth rather
      // than being patched locally into a state the API never confirmed. It is
      // also what draws the pending case durably: a toast lasts seconds, and
      // the "Refund in progress" state on the card is what is still there when
      // the Customer comes back to look.
      router.refresh();
    } catch {
      setError({ code: null, message: null, fallback: "undoNetworkFailed" });
      // A lost response is not proof that nothing happened. The POST may well
      // have reached the API and written a Reversal Request (ADR 0024), and the
      // sentence above — the only thing this dialog can honestly say from here —
      // reads as "it did not work". So the page is asked for the truth on this
      // path too: if a request was made, the card comes back with the refund in
      // progress and without an Undo button to press a second time, and the
      // Customer sees what actually happened rather than what we guessed.
      router.refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => {
          setError(null);
          setOpen(true);
        }}
      >
        {t("undo")}
      </Button>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          // A request in flight is not interruptible: closing the dialog would
          // hide an outcome that is still coming.
          if (busy) return;
          setOpen(next);
        }}
      >
        <DialogContent className="sm:max-w-md" closeLabel={shell("closeLabel")}>
          <DialogHeader>
            <DialogTitle>{t("undoTitle")}</DialogTitle>
            <DialogDescription>
              {/* Two whole paragraphs rather than one with a sentence spliced
                  into the middle of it. The refunded amount lands mid-paragraph
                  in English and need not anywhere else, and a translator handed
                  three fragments to glue cannot move it.

                  No claim about how or when the money arrives: that is the
                  Payment Provider's and the bank's business, and this dialog
                  promises only what the platform itself does. */}
              {paidLabel
                ? t("undoBodyPaid", { event: eventName, amount: paidLabel })
                : t("undoBody", { event: eventName })}
            </DialogDescription>
          </DialogHeader>

          <p className="text-sm text-muted-foreground">
            {t.rich("confirmation", {
              reference: confirmationRef,
              value: (chunks) => (
                <span className="font-mono font-medium text-foreground">{chunks}</span>
              ),
            })}
          </p>

          {error ? (
            <Alert variant="destructive">
              {/* "undo" as the surface: CUSTOMER_SESSION_SCOPE_INSUFFICIENT means
                  a different thing here than it does on "My info", and the API
                  says so with two messages under the one code. */}
              <AlertDescription>
                {apiErrorMessage(errorCopy, error, "undo") ?? t(error.fallback)}
              </AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={busy}>
              {t("undoKeep")}
            </Button>
            <Button variant="destructive" onClick={handleUndo} disabled={busy} aria-busy={busy}>
              {busy ? t("undoing") : t("undo")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
