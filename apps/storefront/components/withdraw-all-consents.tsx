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
import type { WithdrawAll } from "@/lib/customer-session";
import { DATA_PROTECTION_EMAIL } from "@/lib/privacy-policy";

/**
 * Withdraw All on the Privacy page (#269, parent #265): one action that takes
 * back every optional consent, behind the disclosure counsel attaches to it.
 *
 * THE DISCLOSURE IS THE SUBSTANCE OF THIS COMPONENT, not the button. Moving one
 * control carries no duty to explain; taking everything back does, because a
 * Customer performing it is acting on a belief about what will happen next, and
 * the believable version of that belief is wrong. So the dialog exists to say,
 * before anything is written, what stops and what does not.
 *
 * AND THE EASY VERSION OF THAT COPY WOULD BE A LIE. It must not say that
 * processing stops, because it will not stop: somebody who withdraws everything
 * may hold Tickets to an Event next month, and they will keep receiving Sale
 * Confirmations, passcodes and reversal notices — those rest on the contract of
 * selling them a ticket, not on anything they consented to, and counsel's own
 * withdrawal form says withdrawal does not affect access to the platform's
 * essential services. "Passive" (CONTEXT.md) means passive WITH RESPECT TO
 * CONSENT-BASED PROCESSING, and every sentence below is scoped that way.
 *
 * IT IS ALSO THE ONLY PLACE IN THE PRODUCT THAT POINTS AT DELETION. Erasure is
 * a different right with different mechanics and is deliberately not built
 * here; a Customer who wanted it and pressed this instead would leave believing
 * they had asked for something they have not. The last line names the address
 * the published Privacy Policy gives, so the escalation route is real.
 *
 * THE PAGE'S FOOTNOTE AND THIS DIALOG ARE ONE VOICE. The footnote says what is
 * true of turning any single thing off, in the words this dialog reuses
 * verbatim — the same tickets, the same purchase confirmations, sign-in codes
 * and refund notices. What is here and not there is what only the total act
 * needs: both consents named, what is retained and why, and where to write for
 * deletion. Neither may be edited into contradicting the other.
 *
 * DISMISSING IT WRITES NOTHING, and that is structural rather than promised:
 * there is exactly one fetch in this file and it is inside the confirm handler.
 * Opening and closing the dialog issues no request, so a Customer can read what
 * the act means as often as they like and remain in exactly the state they were.
 *
 * ONE ACT IS ONE REQUEST. It calls the Withdraw All route and never the
 * per-purpose control twice: two requests would leave the same Customer in the
 * same state while writing evidence that says they moved two controls, when
 * what happened was one person asking to be left alone.
 *
 * It cannot name whose consents these are. The Customer Session lives in an
 * httpOnly cookie that page scripts never see; this posts to a BFF route that
 * reads the cookie server-side (ADR 0008), and that route decides nothing.
 */
type Envelope = {
  data: WithdrawAll | null;
  error: { code: string; message: string } | null;
};

export function WithdrawAllConsents() {
  const router = useRouter();
  const t = useTranslations("privacySettings");
  const shell = useTranslations("shell");
  const errorCopy = useMessages().errors;
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      // No body. The act names nothing and chooses nothing, so there is nothing
      // for this request to say beyond having been made.
      const response = await fetch("/api/customer/privacy/withdraw-all", { method: "POST" });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("withdrawAllFailed"));
        return;
      }
      setOpen(false);
      // WHICH TOAST IS THE API'S FINDING, NOT THIS COMPONENT'S GUESS. A Customer
      // whose consents were both already denied is sent no confirmation email,
      // because nothing moved — and telling them one is on its way would be the
      // page reporting a change that did not happen.
      const withdrew =
        Boolean(envelope.data?.withdrew.marketing_consent) ||
        Boolean(envelope.data?.withdrew.networking_consent);
      toast.success(withdrew ? t("withdrawAllDoneToast") : t("withdrawAllNothingToast"));
      router.refresh();
    } catch {
      setError(t("withdrawAllNetworkFailed"));
      // A lost response is not proof that nothing happened: the request may well
      // have reached the API and been recorded. The page is asked for the truth
      // rather than left showing what was on screen before.
      router.refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Button
        variant="outline"
        onClick={() => {
          setError(null);
          setOpen(true);
        }}
      >
        {t("withdrawAllAction")}
      </Button>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          // A request in flight is not interruptible: closing now would hide an
          // outcome that is still coming.
          if (busy) return;
          setOpen(next);
        }}
      >
        <DialogContent className="sm:max-w-lg" closeLabel={shell("closeLabel")}>
          <DialogHeader>
            <DialogTitle>{t("withdrawAllTitle")}</DialogTitle>
            <DialogDescription>{t("withdrawAllIntro")}</DialogDescription>
          </DialogHeader>

          {/* The disclosure, as a list rather than a paragraph, because a person
              about to exercise a right should be able to find the one line they
              are worried about. Each is a whole sentence in the catalog: none of
              them is assembled from fragments a translator cannot move. */}
          <ul className="list-disc space-y-2 pl-5 text-sm text-muted-foreground">
            <li>{t("withdrawAllMarketing")}</li>
            <li>{t("withdrawAllNetworking")}</li>
            <li>{t("withdrawAllRetained")}</li>
            <li>{t("withdrawAllUnaffected")}</li>
            <li>{t("withdrawAllReversible")}</li>
          </ul>

          {/* Out of the list and in the reader's own weight, because it is the
              sentence most likely to be the one they actually came for — and the
              only pointer this product has at a right it does not implement. */}
          <p className="text-sm font-medium">
            {t("withdrawAllNotDeletion", { address: DATA_PROTECTION_EMAIL })}
          </p>

          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            {/* Dismissing is the safe half and is offered first, unemphasised:
                nothing is written by pressing it, and nothing has been written
                by having read this far. */}
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={busy}>
              {t("withdrawAllCancel")}
            </Button>
            <Button onClick={confirm} disabled={busy} aria-busy={busy}>
              {busy ? t("withdrawAllWorking") : t("withdrawAllConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
