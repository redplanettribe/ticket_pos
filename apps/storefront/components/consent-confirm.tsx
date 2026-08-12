"use client";

import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { Link } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";

/**
 * The confirmation on the consent landing page (#255, parent #249, ADR 0035):
 * the double opt-in's press.
 *
 * THE PRESS IS THE ACT, AND OPENING THE PAGE IS NOT — the same sentence the
 * unsubscribe page turns on, and here it carries more weight. Mail security
 * scanners open every link in every message before a human sees it; a link that
 * acted on being fetched would GRANT a marketing opt-in nobody ever gave, and
 * leave the platform holding an evidence row saying an inbox confirmed itself
 * when what confirmed it was a robot. So the link lands here, this renders,
 * nothing has happened yet, and the button below sends a POST.
 *
 * It never asks anybody to sign in. The person reading this may have no account
 * at all: a guest checkout creates a Customer nobody has ever signed in as, and
 * the whole point of the link is that pressing it from the inbox is itself the
 * proof of ownership the guest's tick lacked.
 *
 * IT REPORTS THREE OUTCOMES AND NOT TWO. Confirmed, nothing-left-to-confirm, and
 * failed. The middle one is a success — a second press, or an answer the person
 * has already given from their own account — and telling somebody their
 * confirmation failed when it had already worked would be the worst answer this
 * page could give.
 */
type ConsentConfirmProps = {
  /** The signed token the link carried. Never inspected here; only relayed. */
  token: string;
};

type Confirmation = {
  marketing_consent: boolean;
  networking_consent: boolean;
  already_resolved: boolean;
  digest_enabled: boolean;
};

type Envelope = {
  data: Confirmation | null;
  error: { code: string; message: string } | null;
};

export function ConsentConfirm({ token }: ConsentConfirmProps) {
  const t = useTranslations("confirmConsent");
  const errorCopy = useMessages().errors;
  const [state, setState] = useState<"idle" | "busy">("idle");
  const [result, setResult] = useState<Confirmation | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  async function confirm() {
    setState("busy");
    setFailure(null);
    try {
      const response = await fetch("/api/customer/consent/confirm", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error || !envelope.data) {
        setState("idle");
        setFailure(apiErrorMessage(errorCopy, envelope.error) ?? t("failed"));
        return;
      }
      setResult(envelope.data);
    } catch {
      setState("idle");
      setFailure(t("networkFailed"));
    }
  }

  if (result) {
    return (
      <div className="space-y-4">
        <Alert>
          <AlertTitle>
            {result.already_resolved ? t("nothingTitle") : t("doneTitle")}
          </AlertTitle>
          <AlertDescription>
            {/* Said plainly after the fact, because this is the moment somebody
                wonders what they have just agreed to. */}
            {result.already_resolved
              ? t("nothingDescription")
              : t("doneDescription")}
          </AlertDescription>
        </Alert>
        <p className="text-muted-foreground text-sm">
          {/* Reversible, and from where. The Area will ask them to sign in, which
              is acceptable here and not for the press itself: changing a
              standing answer is not the one-click act the link exists to be. */}
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
