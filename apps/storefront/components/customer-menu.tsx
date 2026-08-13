"use client";

import { useTranslations } from "next-intl";
import { useEffect, useRef, useState } from "react";

import { Link, useRouter } from "@/i18n/navigation";

import { CustomerAvatar } from "./customer-avatar";

/**
 * The signed-in chip in the Storefront header: the Customer's Avatar as a
 * button, opening a small menu with the email they are signed in as, the ways
 * into the Customer Area, and the way out.
 *
 * The email caption keeps the old header's guarantee — which identity you are
 * using, never ambiguous — and improves on it: the previous email text was
 * hidden on narrow screens, while the chip is always visible and the menu always
 * names the address.
 *
 * Hand-rolled rather than a menu primitive because the shared UI package
 * carries no dropdown, and one anchored panel with click-outside and Escape is
 * not worth a new dependency.
 */

type CustomerMenuProps = {
  email: string;
  firstName: string;
  lastName: string;
  avatarUrl: string | null;
};

export function CustomerMenu({ email, firstName, lastName, avatarUrl }: CustomerMenuProps) {
  const router = useRouter();
  const t = useTranslations("customerArea");
  // Each entry is named by the surface it opens, from that surface's own key, so
  // a menu item and the heading it lands on cannot come to disagree.
  const myInfo = useTranslations("myInfo");
  // The Following list is the Customer Area's third surface (#218): everything
  // the Customer Follows, in one place, with unfollow available there.
  const following = useTranslations("following");
  // The Privacy page is the Customer Area's fourth surface (#268, parent #265):
  // what this Customer has agreed to, and the controls that change it. It is in
  // the menu because a privacy setting nobody can find is most of the way to not
  // having one — and because the parent spec's whole complaint is that consent
  // could only be changed by accident of history, from a page about follows.
  const privacy = useTranslations("privacySettings");
  const [open, setOpen] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Click-outside and Escape both just close; there is no state worth saving.
  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent | TouchEvent) {
      const container = containerRef.current;
      if (container && event.target instanceof Node && !container.contains(event.target)) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("touchstart", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("touchstart", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  /**
   * Same shape as the old SignOutButton: post to this app's own route handler —
   * the only thing that can clear the httpOnly cookie — then land on the home
   * page as a signed-out visitor.
   */
  async function handleSignOut() {
    setSigningOut(true);
    try {
      await fetch("/api/customer/auth/sign-out", { method: "POST" });
      setOpen(false);
      router.push("/");
      router.refresh();
    } finally {
      setSigningOut(false);
    }
  }

  const itemClass =
    "block w-full rounded-sm px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground";

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("menuLabel", { email })}
        className="flex items-center rounded-full ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
      >
        <CustomerAvatar
          avatarUrl={avatarUrl}
          firstName={firstName}
          lastName={lastName}
          email={email}
        />
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute right-0 top-full z-50 mt-2 w-64 rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
        >
          {/* Which identity you are using, never ambiguous — now on every
              screen size, where the old header hid it on narrow ones. */}
          <p className="truncate px-3 py-2 text-xs text-muted-foreground" title={email}>
            {email}
          </p>
          <div className="my-1 border-t" />
          <Link href="/tickets" role="menuitem" className={itemClass} onClick={() => setOpen(false)}>
            {t("title")}
          </Link>
          <Link
            href="/tickets#my-info"
            role="menuitem"
            className={itemClass}
            onClick={() => setOpen(false)}
          >
            {myInfo("heading")}
          </Link>
          <Link
            href="/following"
            role="menuitem"
            className={itemClass}
            onClick={() => setOpen(false)}
          >
            {following("title")}
          </Link>
          <Link href="/privacy" role="menuitem" className={itemClass} onClick={() => setOpen(false)}>
            {privacy("title")}
          </Link>
          <div className="my-1 border-t" />
          <button
            type="button"
            role="menuitem"
            className={itemClass}
            onClick={handleSignOut}
            disabled={signingOut}
            aria-busy={signingOut}
          >
            {signingOut ? t("signingOut") : t("signOut")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
