"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

// The Sales list (all Members) and the owner-only Sale Import tool are sibling
// sections on the /sales page. When an Org Admin / Event Owner commits or undoes
// a Sale Import, the list must re-fetch its current view so imported sales appear
// and reversed sales drop out — without losing the owner's filters/sort/page.
//
// This context is that coordination seam: the import section calls `notify()` on
// commit/undo success, which bumps a monotonic `signal`. The Sales list depends
// on `signal` in its fetch effect, so a bump re-runs the fetch against whatever
// searchParams (filters/sort/page) are currently active. List state lives in the
// URL, so re-fetching the current view is all that is needed — no reset to page 1
// and no full-page reload.
type SalesRefreshValue = {
  signal: number;
  notify: () => void;
};

const SalesRefreshContext = createContext<SalesRefreshValue>({
  signal: 0,
  // No-op default so a component works even if it is rendered without a provider.
  notify: () => {},
});

export function SalesRefreshProvider({ children }: { children: ReactNode }) {
  const [signal, setSignal] = useState(0);
  const notify = useCallback(() => setSignal((current) => current + 1), []);
  const value = useMemo(() => ({ signal, notify }), [signal, notify]);

  return <SalesRefreshContext.Provider value={value}>{children}</SalesRefreshContext.Provider>;
}

// useSalesRefreshSignal returns a value that changes whenever the list should
// re-fetch its current view. Include it in the fetch effect's dependency array.
export function useSalesRefreshSignal(): number {
  return useContext(SalesRefreshContext).signal;
}

// useSalesRefreshNotify returns the callback the import section calls after a
// successful commit or undo to trigger the Sales list to re-fetch.
export function useSalesRefreshNotify(): () => void {
  return useContext(SalesRefreshContext).notify;
}
