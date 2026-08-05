"use client";

import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { Link } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";

/**
 * The confirmation on the unsubscribe landing page (#224, parent #215,
 * ADR 0030).
 *
 * THE PRESS IS THE ACT, AND OPENING THE PAGE IS NOT. That sentence is the entire
 * reason this component exists instead of the link simply working. Mail security
 * scanners open every link in every message before a human sees it; a link that
 * acted on being fetched would mean a Customer at a company running one could
 * never receive a second Digest, and nobody on either side would ever learn why.
 * So the link lands here, this renders, nothing has happened yet, and the button
 * below sends a POST.
 *
 * It never asks anybody to sign in. The token that came out of the email is the
 * whole authority — a Digest is read months after anybody last signed in, and an
 * opt-out behind a passcode is not an opt-out.
 *
 * It says plainly that the Follows survive, before and after. Unsubscribe and
 * Unfollow are different acts (CONTEXT.md), and a person who fears losing what
 * they follow will simply never press this.
 */
type UnsubscribeConfirmProps = {
  /** The signed token the link carried. Never inspected here; only relayed. */
  token: string;
};

type Envelope = {
  error: { code: string; message: string } | null;
};

export function UnsubscribeConfirm({ token }: UnsubscribeConfirmProps) {
  const t = useTranslations("unsubscribe");
  const errorCopy = useMessages().errors;
  const [state, setState] = useState<"idle" | "busy" | "done">("idle");
  const [failure, setFailure] = useState<string | null>(null);

  async function confirm() {
    setState("busy");
    setFailure(null);
    try {
      const response = await fetch("/api/customer/unsubscribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        setState("idle");
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t("failed"));
        return;
      }
      setState("done");
    } catch {
      setState("idle");
      setFailure(t("networkFailed"));
    }
  }

  if (state === "done") {
    return (
      <div className="space-y-4">
        <Alert>
          <AlertTitle>{t("doneTitle")}</AlertTitle>
          {/* Said again after the fact, because this is the moment somebody
              wonders what they just gave up. */}
          <AlertDescription>{t("doneDescription")}</AlertDescription>
        </Alert>
        <p className="text-muted-foreground text-sm">
          {/* Reversible, and from where. The link is to the Customer Area, which
              will ask them to sign in — acceptable here and not for the
              unsubscribe itself, because turning mail back ON is a request to be
              written to and takes the proof pressing Follow does. */}
          <Link href="/following" className="underline">
            {t("manageLink")}
          </Link>
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {failure ? (
        <Alert variant="destructive">
          <AlertTitle>{t("failedTitle")}</AlertTitle>
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      ) : null}
      <Button onClick={confirm} disabled={state === "busy"}>
        {t("confirm")}
      </Button>
    </div>
  );
}
