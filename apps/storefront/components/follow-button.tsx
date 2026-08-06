"use client";

import { Button, toast } from "@ticket-pos/ui";
import { Heart } from "lucide-react";
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
 * Tag Follows arrived and needed one thing this did not have: a form small
 * enough to sit beside a chip. `compact` is that form — the same button, the
 * same request, the same optimism, without the sentence underneath, since the
 * chip it sits against already names the subject. It is a second SIZE rather
 * than a second control, which is the distinction worth keeping: every rule
 * about what pressing this means is stated once.
 *
 * It still says the word. Dropping to the heart alone is the obvious next step
 * beside a chip and is deliberately not taken here: an unlabelled heart is the
 * "favourite" reading this feature spent a design decision avoiding, and it
 * would be a UI change to the tag explorer rather than to this control.
 *
 * When it is compact the subject's name moves into `aria-label`, because a
 * screen reader gets no help from the chip sitting next to it and "Follow" alone
 * would be the ambiguity a sighted reader does not have.
 *
 * It is drawn for ANYBODY, signed in or not (#219). The surfaces it lives on are
 * overwhelmingly anonymous, so a control only signed-in visitors could see would
 * be invisible to almost everyone it is for. Pressing it while signed out is a
 * navigation rather than a write: it carries the intended Follow and the page it
 * was pressed on into the existing sign-in flow, which does the writing on the
 * far side against the session it mints. See lib/follow-intent.ts.
 *
 * The two axes are independent: a compact control on a tag chip is pressed by
 * anonymous visitors just as a full one on an Organization page is, so size and
 * signed-in-ness compose rather than one implying the other.
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
   * The small form, for sitting beside a Tag chip: no hint sentence, no visible
   * label, and the subject's name carried in `aria-label` instead. Everything
   * else — the request, the optimistic state, the error copy — is unchanged.
   */
  compact?: boolean;
  /**
   * The intended Follow as it travels through sign-in: "organization:<slug>" or
   * "tag:<key>". Built by the page from the same identifier the endpoint above
   * is built from, so the two cannot name different things.
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

/**
 * The heart beside the word, outlined while not Following and filled once
 * Following.
 *
 * It carries the STATE and not the meaning. The meaning is the word next to it,
 * which stays "Follow" throughout (CONTEXT.md) — a heart alone would read as
 * "favourite", the shortlist idea this feature deliberately is not, and the fill
 * is only doing what `aria-pressed` already does for anyone not looking at it.
 * Hence aria-hidden: a screen reader that has just been told the button is
 * pressed gains nothing from a second announcement of the same fact, and the
 * accessible name is left as the word alone, which is what the Playwright
 * journey and every translation still address it by.
 *
 * Sized by the Button's own `[&_svg]:size-4`, not overridden here. Discretion is
 * the outline and the inherited colour doing it; a one-off smaller icon would
 * only be this control disagreeing with every other icon in the design system.
 */
function FollowHeart({ following }: { following: boolean }) {
  return <Heart aria-hidden="true" className={following ? "fill-current" : undefined} />;
}

export function FollowButton({
  endpoint,
  following,
  subjectName,
  compact = false,
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

  const button = (
    <Button
      // Following is the settled state and reads as such; not following is the
      // invitation, and gets the emphasis.
      variant={isFollowing ? "outline" : "default"}
      size="sm"
      aria-pressed={isFollowing}
      // Named for a screen reader only where the label is not the name: beside a
      // chip the button says "Follow" and the chip says which, which no reader
      // of the accessibility tree alone can put together.
      aria-label={
        compact
          ? isFollowing
            ? t("followingLabel", { name: subjectName })
            : t("followLabel", { name: subjectName })
          : undefined
      }
      disabled={busy || pending}
      onClick={toggle}
    >
      <FollowHeart following={isFollowing} />
      {isFollowing ? t("following") : t("follow")}
    </Button>
  );

  // Signed out: the same control, saying the same word, but a way into sign-in
  // rather than a write. A plain Link and not a fetch — the visitor is leaving
  // this page, and the whole point is that they come back to it with the Follow
  // already made.
  //
  // The hint is the one thing said differently, and it is worth saying: somebody
  // about to be sent to a passcode form should be told that is what pressing
  // this does, rather than discovering it. Beside a chip there is no room to say
  // it, so compact drops the sentence here exactly as it does when signed in —
  // the page that has room says it once.
  if (!signedIn) {
    const link = (
      <Button
        asChild
        variant="default"
        size="sm"
        aria-label={compact ? t("followLabel", { name: subjectName }) : undefined}
      >
        <Link href={followSignInHref(intent, pathname, searchParams.toString())}>
          {/* Always the outline: a visitor who is not signed in Follows nothing
              yet, whatever they are about to do on the far side of sign-in. */}
          <FollowHeart following={false} />
          {t("follow")}
        </Link>
      </Button>
    );

    if (compact) {
      return link;
    }

    return (
      <div className="space-y-1">
        {link}
        <p className="text-muted-foreground text-xs">{t("signInHint")}</p>
      </div>
    );
  }

  // Compact: the button alone. The hint belongs to a page that has room to
  // explain what a Follow is, and is said once there rather than once per chip.
  if (compact) {
    return button;
  }

  return (
    <div className="space-y-1">
      {button}
      <p className="text-muted-foreground text-xs">
        {isFollowing ? t("followingHint") : t("followHint")}
      </p>
    </div>
  );
}
