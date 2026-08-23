"use client";

import { Button } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";
import { useState, type ReactNode } from "react";

import { useRouter } from "@/i18n/navigation";
import { DEFAULT_DESTINATION, SIGN_IN_PATH, safeNext } from "@/lib/destination";

/**
 * Leaves the current Customer Session and lands on a usable sign-in form, for
 * someone whose tickets sit under a different email address — or who is about to
 * buy under the wrong one.
 *
 * A plain link to /signin cannot do this. Anyone reading the empty Customer Area
 * is by definition holding a full Customer Session, and /signin bounces exactly
 * those visitors to their destination — so the link would return them to the
 * empty state having achieved nothing. The session has to end first. The
 * checkout dialog needs it for the same reason and a sharper one: it is the way
 * out of a purchase about to be addressed to somebody else's inbox (ADR 0054),
 * and it must not bounce straight back into that dialog.
 *
 * It ends the same way the header's sign-out does: a POST to this app's own
 * route handler, which destroys the server-side session row and clears the
 * httpOnly cookie. A GET link would have been simpler and wrong twice over —
 * Next prefetches links, so scrolling past this empty state would sign people
 * out, and a session-destroying GET is reachable from any other site's markup.
 */
export function SignInOtherAddressButton({
  label,
  next = DEFAULT_DESTINATION,
}: {
  /**
   * Where the new session should land. Defaults to the Customer Area, which is
   * what the empty state wants; the checkout dialog passes the Event page,
   * basket and all, so that changing your mind about which account you are
   * costs you nothing you had already chosen (lib/checkout-signin.ts).
   *
   * Guarded by `safeNext` on the way into the address, like every other `next`
   * this app writes.
   */
  next?: string;
  /**
   * What the button reads while it is idle, handed down by the sentence it sits
   * inside. The offer is one clause of that sentence, so its words belong to
   * the sentence's message rather than to this component — which is what lets a
   * language put the clause wherever it needs to go.
   */
  label: ReactNode;
}) {
  const router = useRouter();
  const t = useTranslations("customerArea");
  const [loading, setLoading] = useState(false);

  async function handleSwitchAddress() {
    setLoading(true);
    try {
      await fetch("/api/customer/auth/sign-out", { method: "POST" });
      router.push(`${SIGN_IN_PATH}?next=${encodeURIComponent(safeNext(next))}`);
      // The router cache still holds pages rendered for the session just
      // destroyed; refresh discards them so /signin re-reads a cookie that is
      // now gone and renders the form.
      router.refresh();
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button
      type="button"
      variant="link"
      className="h-auto p-0 text-sm font-normal"
      onClick={handleSwitchAddress}
      disabled={loading}
      aria-busy={loading}
    >
      {/* The busy state is this component's own business and never appears in
          the sentence, so it comes from the catalog directly. */}
      {loading ? t("signingOut") : label}
    </Button>
  );
}
