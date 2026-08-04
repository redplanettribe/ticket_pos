"use client";

import { Button, toast } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useMessages, useTranslations } from "next-intl";
import { useState, useTransition } from "react";

import { apiErrorMessage } from "@/lib/api-errors";

/**
 * The Follow control (#217, parent #215).
 *
 * One control for both directions rather than two buttons: it shows the current
 * state and pressing it moves to the other one, because "Following" and
 * "Unfollow" are the same fact seen from either side and a Customer who has
 * followed something needs to be told so in the place they would press to undo
 * it.
 *
 * The word is FOLLOW throughout, never "save", "favorite" or "subscribe"
 * (CONTEXT.md). A Follow is a standing subscription whose whole payload is the
 * Follow Digest — pressing it is a request to be written to, not a bookmark —
 * and the copy says as much beneath the button so nobody presses it expecting a
 * shortlist.
 *
 * DELIBERATELY GENERIC in what it follows. It takes the address to write to and
 * the name to speak about, so the same control goes on an Event page and on an
 * explorer tag chip when Tag Follows land (#218) without being rewritten. What
 * it does not take is any notion of Customer: the session cookie is the whole of
 * the authorization and it never reaches page scripts, so this component cannot
 * name whose Follow it is even by mistake.
 *
 * It is drawn only for a signed-in Customer. Following while signed out is its
 * own problem — the press has to survive a round trip through sign-in — and is
 * #219, not this.
 */
type FollowButtonProps = {
  /**
   * The BFF path this control follows and unfollows through: POST to follow,
   * DELETE to unfollow. Passed in rather than built here so a second kind of
   * Follow needs no change to this file.
   */
  endpoint: string;
  /** Whether the Customer Follows it right now, as the server rendered it. */
  following: boolean;
  /** What is being followed, named for the screen reader and for the toast. */
  subjectName: string;
};

type Envelope = {
  error: { code: string; message: string } | null;
};

export function FollowButton({ endpoint, following, subjectName }: FollowButtonProps) {
  const router = useRouter();
  const t = useTranslations("follow");
  // Keys of API codes rather than message keys, so the catalog is read as plain
  // data rather than through `t`.
  const errorCopy = useMessages().errors;
  // The server's answer is the truth, and `router.refresh()` is what fetches the
  // next one — but it lands a beat later than the press. This holds the state
  // the press asked for in the meantime so the button does not sit visibly wrong
  // while the page re-renders, and it is re-seeded from the prop below.
  const [optimistic, setOptimistic] = useState<boolean | null>(null);
  const [pending, startTransition] = useTransition();
  const [busy, setBusy] = useState(false);

  const isFollowing = optimistic ?? following;

  async function toggle() {
    const next = !isFollowing;
    setBusy(true);
    setOptimistic(next);
    try {
      const response = await fetch(endpoint, { method: next ? "POST" : "DELETE" });
      const envelope = (await response.json()) as Envelope;
      if (!response.ok || envelope.error) {
        // Every reason this can fail is the API's — the session is too narrow,
        // the Organization is gone, the session expired — and its code is which
        // of them happened. Choosing the words for it is this app's; deciding it
        // again here would only let the two disagree (ADR 0023).
        setOptimistic(null);
        toast.error(apiErrorMessage(errorCopy, envelope.error, "follow") ?? t("failed"));
        return;
      }
      toast.success(next ? t("followedToast", { name: subjectName }) : t("unfollowedToast", { name: subjectName }));
    } catch {
      setOptimistic(null);
      toast.error(t("networkFailed"));
    } finally {
      setBusy(false);
      // The control is server-rendered from the Follows read, so the page is
      // asked for the new truth rather than left on a local guess. It also
      // repairs the optimistic state on the paths above that could not.
      startTransition(() => router.refresh());
    }
  }

  return (
    <div className="space-y-1">
      <Button
        // Following is the settled state and reads as such; not following is the
        // invitation, and gets the emphasis.
        variant={isFollowing ? "outline" : "default"}
        size="sm"
        aria-pressed={isFollowing}
        disabled={busy || pending}
        onClick={toggle}
      >
        {isFollowing ? t("following") : t("follow")}
      </Button>
      <p className="text-muted-foreground text-xs">
        {isFollowing ? t("followingHint") : t("followHint")}
      </p>
    </div>
  );
}
