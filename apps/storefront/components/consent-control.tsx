"use client";

import { Button, toast } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useMessages, useTranslations } from "next-intl";
import { useState, useTransition } from "react";

import { apiErrorMessage } from "@/lib/api-errors";
import type { ConsentState } from "@/lib/customer-session";

/**
 * One optional consent on the Privacy page, and the control that moves it
 * (#268, parent #265).
 *
 * IT REPORTS FOUR STATES AND DRAWS THEM DIFFERENTLY, because they mean four
 * different things and two of them are not answers at all. `unanswered` is a
 * question nobody has asked this Customer; `pending_confirmation` is somebody
 * else's tick, standing unresolved against their address because a guest typed
 * it at a checkout (ADR 0035). Rendering either as "off" would tell a person
 * they had refused something they were never asked about, or settle on their
 * behalf a question that is still open.
 *
 * SHOWING THE STATE IS NOT PRE-TICKING A BOX. The never-pre-tick rule governs
 * the moments where the platform ASKS — sign-in and checkout, where a box's
 * position is what a person's answer will be recorded as. This is a settings
 * surface reporting what is true, and the difference is visible in the markup:
 * there is no checkbox and no form here, only a sentence saying where things
 * stand and a button that performs one act.
 *
 * WHICH BUTTONS APPEAR IS THE WHOLE OF HOW THIS PAGE AVOIDS ASKING:
 *
 *   - `granted` offers "turn off" alone. Turning it on again would be a request
 *     to record an answer already standing.
 *   - `denied` offers "turn on" alone, which is the route back that exists
 *     nowhere else — every surface that grants an optional consent shows the box
 *     only while the state is unanswered, so without this a Customer who
 *     withdrew by mistake could never be shown it again.
 *   - `unanswered` offers "turn on" ALONE, and that absence is deliberate. A
 *     "turn off" here would let somebody record a refusal to a question the
 *     platform never put to them, which is the page asking — and this page must
 *     never be the surface that first asks a Customer anything.
 *   - `pending_confirmation` offers BOTH, because something IS standing open and
 *     the only person entitled to settle it is the one reading this. Either
 *     answer resolves it: yes makes it theirs, no takes away a tick that was
 *     never theirs.
 *
 * The optimistic-then-refresh shape is the digest toggle's, for the same
 * reason: the server's answer is the truth and `router.refresh()` fetches the
 * next one, but it lands a beat after the press and a control that visibly sat
 * wrong in between would be read as the press not having worked. The optimistic
 * value is only ever `granted` or `denied` — the two states a proven answer can
 * produce — and it is dropped the moment the API disagrees.
 *
 * It cannot name whose consent it is. The Customer Session lives in an httpOnly
 * cookie that page scripts never see; this posts to a BFF route that reads the
 * cookie server-side (ADR 0008), and that route decides nothing.
 */
type ConsentControlProps = {
  /** Which consent this is, as the API's own path vocabulary names it. */
  purpose: "marketing" | "networking";
  /** Its state right now, as the server rendered it. */
  state: ConsentState;
  /** What this consent is called, for the heading and the toast. */
  title: string;
  /** What agreeing to it authorizes, in one sentence. */
  description: string;
};

type Envelope = {
  data: { marketing_consent: ConsentState; networking_consent: ConsentState } | null;
  error: { code: string; message: string } | null;
};

export function ConsentControl({ purpose, state, title, description }: ConsentControlProps) {
  const router = useRouter();
  const t = useTranslations("privacySettings");
  const errorCopy = useMessages().errors;
  const [optimistic, setOptimistic] = useState<ConsentState | null>(null);
  const [pending, startTransition] = useTransition();
  const [busy, setBusy] = useState(false);

  const current = optimistic ?? state;

  async function move(granted: boolean) {
    setBusy(true);
    // Only ever the two states a proven answer produces. A press cannot leave a
    // consent pending or unanswered, so nothing else is a truthful guess.
    setOptimistic(granted ? "granted" : "denied");
    try {
      const response = await fetch(`/api/customer/privacy/consents/${purpose}`, {
        // A STATE, never a flip: a retried or double-tapped request means the
        // same thing once, where a flip would grant back what somebody had just
        // taken away.
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ granted }),
      });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        setOptimistic(null);
        toast.error(apiErrorMessage(errorCopy, envelope.error) ?? t("changeFailed"));
        return;
      }
      // The API's own finding about the two consents, which is the truth and is
      // not necessarily what was asked for.
      const settled = envelope.data?.[`${purpose}_consent`];
      if (settled) setOptimistic(settled);
      // Withdrawing is confirmed by email and granting is not — the API's rule,
      // and the toast only reports which act the reader just performed.
      toast.success(
        granted
          ? t("grantedToast", { consent: title })
          : t("withdrawnToast", { consent: title }),
      );
    } catch {
      setOptimistic(null);
      toast.error(t("changeNetworkFailed"));
    } finally {
      setBusy(false);
      startTransition(() => router.refresh());
    }
  }

  const disabled = busy || pending;

  // The four states as four sentences, never as three plus a default, and
  // written out here rather than resolved through a helper: the message keys are
  // typechecked against en.json (messages/README.md), which only holds while
  // they are literals at the call site.
  const stateSentence =
    current === "granted"
      ? t("stateGranted")
      : current === "denied"
        ? t("stateDenied")
        : current === "pending_confirmation"
          ? t("statePending")
          : t("stateUnanswered");

  return (
    <div className="flex flex-col gap-3 rounded-lg border p-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0 space-y-1">
        <p className="font-medium">{title}</p>
        <p className="text-muted-foreground text-sm">{description}</p>
        {/* The state in words. A Pending Confirmation gets the longest sentence
            of the four, because it is the only one where something happened
            that the reader did not do and would otherwise have no way to
            understand. */}
        <p className="text-sm">{stateSentence}</p>
      </div>
      <div className="flex shrink-0 gap-2">
        {current === "pending_confirmation" ? (
          <>
            <Button size="sm" disabled={disabled} onClick={() => move(true)}>
              {t("pendingConfirm")}
            </Button>
            <Button variant="outline" size="sm" disabled={disabled} onClick={() => move(false)}>
              {t("pendingRefuse")}
            </Button>
          </>
        ) : current === "granted" ? (
          // On is the settled state and reads as such; the act available from it
          // is the withdrawal, which is not the thing being invited.
          <Button variant="outline" size="sm" disabled={disabled} onClick={() => move(false)}>
            {t("withdraw")}
          </Button>
        ) : (
          // `denied` and `unanswered` both offer only this. See the note above
          // for why an unanswered consent gets no "turn off".
          <Button size="sm" disabled={disabled} onClick={() => move(true)}>
            {t("grant")}
          </Button>
        )}
      </div>
    </div>
  );
}
