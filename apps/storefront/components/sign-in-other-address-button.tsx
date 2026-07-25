"use client";

import { Button } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";

/**
 * Leaves the current Customer Session and lands on a usable sign-in form, for
 * someone whose tickets sit under a different email address.
 *
 * A plain link to /signin cannot do this. Anyone reading the empty Customer Area
 * is by definition holding a full Customer Session, and /signin bounces exactly
 * those visitors to their destination — so the link would return them to the
 * empty state having achieved nothing. The session has to end first.
 *
 * It ends the same way the header's sign-out does: a POST to this app's own
 * route handler, which destroys the server-side session row and clears the
 * httpOnly cookie. A GET link would have been simpler and wrong twice over —
 * Next prefetches links, so scrolling past this empty state would sign people
 * out, and a session-destroying GET is reachable from any other site's markup.
 */
export function SignInOtherAddressButton() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);

  async function handleSwitchAddress() {
    setLoading(true);
    try {
      await fetch("/api/customer/auth/sign-out", { method: "POST" });
      router.push("/signin?next=/tickets");
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
      {loading ? "Signing out…" : "Sign out and sign in with that address"}
    </Button>
  );
}
