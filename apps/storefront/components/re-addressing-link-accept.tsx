"use client";

import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { useRouter } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import {
  reAddressingLinkFailure,
  reAddressingLinkNext,
  type ReAddressingAccepted,
} from "@/lib/re-addressing-link";

/**
 * Accepting a purchase re-addressed to you (#422, parent #419, ADR 0058).
 *
 * THE PRESS IS THE ACT, AND OPENING THE PAGE IS NOT — the same sentence the
 * Assignment accept page turns on. Mail security scanners open every link in
 * every message before a human sees it; a page that accepted on being fetched
 * would move a Sale to an address nobody proved. So the page renders what the
 * view read said, nothing has happened, and the button below sends a POST.
 *
 * IT HAS ONE BUTTON AND NO FIELDS. There is no name to give and no question to
 * answer here: the Sale carries its own snapshot, and the Customer Area the
 * press lands on is where assigning and answering happen. Anyone adding a
 * field here is turning an acceptance into a registration.
 *
 * ON SUCCESS IT NAVIGATES AND RENDERS NOTHING OF ITS OWN. The BFF has set the
 * session (or parked the consent step) exactly as a passcode sign-in does, and
 * the destination — the Sale in the Customer Area, or the sign-in page's
 * consent step bound for that Sale — is decided by lib/re-addressing-link.
 * `router.refresh()` follows the push as it does on the sign-in form, so the
 * shell's Customer navigation re-renders with the cookie the press just set.
 */
type ReAddressingLinkAcceptProps = {
  /** The signed token the link carried. Never inspected here; only relayed. */
  token: string;
  /** Whether the view read already showed the link accepted: the label changes, the act does not. */
  accepted: boolean;
};

type Envelope = {
  data: ReAddressingAccepted | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export function ReAddressingLinkAccept({ token, accepted }: ReAddressingLinkAcceptProps) {
  const t = useTranslations("reAddressingLink");
  const errorCopy = useMessages().errors;
  const router = useRouter();

  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function accept() {
    setBusy(true);
    setFailure(null);
    try {
      const response = await fetch("/api/re-addressing-link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        // The token travels in the BODY on the browser leg too: a credential in
        // a URL ends up in a Referer header on the way to wherever the reader
        // goes next, and this one asserts who somebody is.
        body: JSON.stringify({ token }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error || !envelope.data) {
        // The API's code and reason choose the copy: a withdrawn re-addressing,
        // a reversed Sale and a started Event are each said plainly, because
        // this reader is the buyer and the facts are their own.
        const key = reAddressingLinkFailure(envelope.error?.code, envelope.error?.details);
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t(`${key}Description`));
        return;
      }
      router.push(reAddressingLinkNext(envelope.data));
      router.refresh();
      // `busy` is left set on purpose: the page is on its way out, and a button
      // that re-enabled itself before the navigation landed would invite the
      // second press the API tolerates but the reader does not need.
      return;
    } catch {
      setFailure(t("networkFailed"));
    }
    setBusy(false);
  }

  return (
    <div className="space-y-4">
      {failure ? (
        <Alert variant="destructive">
          <AlertTitle>{t("failedTitle")}</AlertTitle>
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      ) : null}
      {/* Said BEFORE the button and never after it. Somebody deciding whether
          to press is entitled to know what pressing does, and a sentence below
          the control is a sentence half of them meet too late. */}
      <p className="text-muted-foreground text-sm">
        {accepted ? t("acceptedDisclosure") : t("disclosure")}
      </p>
      <Button onClick={accept} disabled={busy} aria-busy={busy}>
        {accepted ? t("open") : t("accept")}
      </Button>
      {/* Ignoring the mail IS the decline — there is no decline button here or
          anywhere, deliberately. This is where that is said. */}
      {accepted ? null : <p className="text-muted-foreground text-sm">{t("ignoring")}</p>}
    </div>
  );
}
