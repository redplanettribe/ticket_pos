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
import { useState } from "react";

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
 * rendered before the deadline and pressed after it comes back refused with a
 * message rather than quietly succeeding. That refusal is shown verbatim.
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

type Envelope = {
  data: unknown;
  error: { code: string; message: string } | null;
};

export function UndoPurchase({
  saleId,
  eventName,
  confirmationRef,
  paidLabel = null,
}: UndoPurchaseProps) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
        // sale is already undone, the payment cannot be reversed — and it says
        // so in one sentence. Repeating that judgement here would only let the
        // two disagree.
        setError(envelope.error?.message ?? "This purchase could not be undone. Try again.");
        return;
      }
      setOpen(false);
      toast.success("Your purchase is undone.");
      // The card, the badge and the ticket lists are all server-rendered from
      // the Customer Area read, so the page is asked for the new truth rather
      // than being patched locally into a state the API never confirmed.
      router.refresh();
    } catch {
      setError("This purchase could not be undone. Check your connection and try again.");
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
        Undo this purchase
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
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Undo this purchase?</DialogTitle>
            <DialogDescription>
              Your tickets for {eventName} will be released and this purchase will be marked
              reversed.{" "}
              {/* No claim about how or when the money arrives: that is the
                  Payment Provider's and the bank's business, and this dialog
                  promises only what the platform itself does. */}
              {paidLabel ? `Your ${paidLabel} payment will be returned. ` : null}
              You can&apos;t take this back — if the tickets sell out, you won&apos;t be able to get
              them again.
            </DialogDescription>
          </DialogHeader>

          <p className="text-sm text-muted-foreground">
            Confirmation{" "}
            <span className="font-mono font-medium text-foreground">{confirmationRef}</span>
          </p>

          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={busy}>
              Keep my tickets
            </Button>
            <Button variant="destructive" onClick={handleUndo} disabled={busy}>
              {busy ? "Undoing…" : "Undo this purchase"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
