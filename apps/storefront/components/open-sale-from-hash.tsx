"use client";

import { useEffect } from "react";

/**
 * Opens the Ticket Sale row a URL fragment names (#354).
 *
 * A Confirmation Link and the redirect after an undo both land on
 * `/tickets#sale-<id>` (lib/destination), and the browser scrolls to that id
 * on its own. But since #354 the Sale's details sit inside a `<details>`
 * that is shut whenever its Event has more than one Sale, and a fragment
 * cannot open one — the browser scrolls to a closed row and the reader sees
 * the Event, not the purchase they were sent to. So this opens it, then
 * scrolls again: the first scroll happened before the row grew.
 *
 * It draws nothing and touches only the element the fragment names; any
 * other id on the page is left to the browser. `hashchange` covers a
 * fragment changed in place — the undo redirect arriving on a page that is
 * already open, or two Confirmation Links followed in turn.
 */
export function OpenSaleFromHash() {
  useEffect(() => {
    const open = () => {
      const id = window.location.hash.slice(1);
      if (!id) return;
      const target = document.getElementById(id);
      if (!(target instanceof HTMLDetailsElement)) return;
      target.open = true;
      target.scrollIntoView();
    };
    open();
    window.addEventListener("hashchange", open);
    return () => window.removeEventListener("hashchange", open);
  }, []);
  return null;
}
