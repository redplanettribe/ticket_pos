"use client";

import { Button, toast } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useMessages, useTranslations } from "next-intl";
import { useState, useTransition } from "react";

import { apiErrorMessage } from "@/lib/api-errors";

/**
 * The Follow Digest switch, beside the Following list (#224, parent #215,
 * ADR 0030).
 *
 * IT SAYS WHAT IT DOES NOT DO, and that is most of why it exists as its own
 * control with its own sentence rather than as a bare switch. Unsubscribe and
 * Unfollow are different acts (CONTEXT.md): turning this off silences the weekly
 * email and leaves every Follow standing, and the list underneath is the proof.
 * A person who could not tell the two apart would have exactly one safe move
 * available to them — never press it, and ignore the mail — which is the outcome
 * an opt-out exists to avoid.
 *
 * It is drawn from the SAME read as the list beside it (`digest_enabled` rides
 * on the Follows listing), so the switch and the list cannot disagree about what
 * this Customer's state is.
 *
 * The same optimistic-then-refresh shape as the Follow control, for the same
 * reason: the server's answer is the truth and `router.refresh()` fetches the
 * next one, but it lands a beat after the press, and a switch that visibly sat
 * wrong in between would be read as the press not having worked.
 *
 * It cannot name whose switch it is. The Customer Session lives in an httpOnly
 * cookie that page scripts never see; this posts to a BFF route that reads the
 * cookie server-side (ADR 0008).
 */
type DigestToggleProps = {
  /** Whether the Digest is on right now, as the server rendered it. */
  enabled: boolean;
};

type Envelope = {
  error: { code: string; message: string } | null;
};

export function DigestToggle({ enabled }: DigestToggleProps) {
  const router = useRouter();
  const t = useTranslations("following");
  const errorCopy = useMessages().errors;
  const [optimistic, setOptimistic] = useState<boolean | null>(null);
  const [pending, startTransition] = useTransition();
  const [busy, setBusy] = useState(false);

  const isEnabled = optimistic ?? enabled;

  async function toggle() {
    const next = !isEnabled;
    setBusy(true);
    setOptimistic(next);
    try {
      const response = await fetch("/api/customer/digest", {
        // A STATE, never a flip: a retried or double-tapped request means the
        // same thing once, where a flip would resubscribe somebody who asked
        // twice for quiet.
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: next }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        setOptimistic(null);
        toast.error(apiErrorMessage(errorCopy, envelope.error) ?? t("digestFailed"));
        return;
      }
      toast.success(next ? t("digestOnToast") : t("digestOffToast"));
    } catch {
      setOptimistic(null);
      toast.error(t("digestNetworkFailed"));
    } finally {
      setBusy(false);
      startTransition(() => router.refresh());
    }
  }

  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-4">
      <div className="min-w-0 space-y-1">
        <p className="font-medium">{t("digestTitle")}</p>
        {/* The state in words, and — when it is off — the reassurance that the
            list below is untouched. That second sentence is the whole point of
            this control having copy at all. */}
        <p className="text-muted-foreground text-sm">
          {isEnabled ? t("digestOnDescription") : t("digestOffDescription")}
        </p>
      </div>
      <Button
        // On is the settled state and reads as such; off is what invites a
        // press, exactly as the Follow control is shaped.
        variant={isEnabled ? "outline" : "default"}
        size="sm"
        aria-pressed={isEnabled}
        disabled={busy || pending}
        onClick={toggle}
      >
        {isEnabled ? t("digestTurnOff") : t("digestTurnOn")}
      </Button>
    </div>
  );
}
