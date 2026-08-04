"use client";

import { Button, toast } from "@ticket-pos/ui";
import { useRouter, useSearchParams } from "next/navigation";
import { useMessages, useTranslations } from "next-intl";
import { useState, useTransition } from "react";

import { Link, usePathname } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";
import { followSignInHref } from "@/lib/follow-intent";

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
 * It is drawn for ANYBODY, signed in or not (#219). The surfaces it lives on are
 * overwhelmingly anonymous, so a control only signed-in visitors could see would
 * be invisible to almost everyone it is for. Pressing it while signed out is a
 * navigation rather than a write: it carries the intended Follow and the page it
 * was pressed on into the existing sign-in flow, which does the writing on the
 * far side against the session it mints. See lib/follow-intent.ts.
 *
 * Which of the two it is, is decided by the server that rendered it — this
 * component is told, and never reads a cookie or a session to find out. The
 * Customer Session lives in an httpOnly cookie that page scripts cannot see, and
 * that is the property being preserved: nothing here can name whose Follow this
 * would be even by mistake.
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
  /**
   * The intended Follow as it travels through sign-in: "organization:<slug>",
   * and "tag:<key>" when Tag Follows land (#218). Built by the page from the
   * same slug the endpoint above is built from, so the two cannot name different
   * things.
   */
  intent: string;
  /**
   * Whether the visitor holds a full Customer Session, as the server rendering
   * this knew it. False makes the control a way into sign-in rather than a
   * write — and it is a rendering decision only: the API refuses an
   * unauthenticated Follow regardless, so nothing here is an access control.
   */
  signedIn: boolean;
};

type Envelope = {
  error: { code: string; message: string } | null;
};

export function FollowButton({
  endpoint,
  following,
  subjectName,
  intent,
  signedIn,
}: FollowButtonProps) {
  const router = useRouter();
  const t = useTranslations("follow");
  // Where the visitor is, for the `next` that brings them back here. Only the
  // browser's router knows which page this is, which is why the address is read
  // here and the rules about what may be in it live in lib/follow-intent.ts.
  const pathname = usePathname();
  const searchParams = useSearchParams();
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

  // Signed out: the same control, saying the same word, but a way into sign-in
  // rather than a write. A plain Link and not a fetch — the visitor is leaving
  // this page, and the whole point is that they come back to it with the Follow
  // already made.
  //
  // The hint below is the one thing said differently, and it is worth saying:
  // somebody about to be sent to a passcode form should be told that is what
  // pressing this does, rather than discovering it.
  if (!signedIn) {
    return (
      <div className="space-y-1">
        <Button asChild variant="default" size="sm">
          <Link href={followSignInHref(intent, pathname, searchParams.toString())}>
            {t("follow")}
          </Link>
        </Button>
        <p className="text-muted-foreground text-xs">{t("signInHint")}</p>
      </div>
    );
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
