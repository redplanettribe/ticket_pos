"use client";

import { Button } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";

/**
 * Signs the Customer out of the Storefront.
 *
 * It posts to this app's own route handler, which is the only thing that can
 * touch the httpOnly cookie — there is no client-readable token for this button
 * to clear, by design. Any Staff Session is untouched: that cookie belongs to a
 * different origin and a different table (ADR 0010).
 */
export function SignOutButton() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);

  async function handleSignOut() {
    setLoading(true);
    try {
      await fetch("/api/customer/auth/sign-out", { method: "POST" });
      router.push("/");
      router.refresh();
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      onClick={handleSignOut}
      disabled={loading}
      aria-busy={loading}
    >
      {loading ? "Signing out…" : "Sign out"}
    </Button>
  );
}
