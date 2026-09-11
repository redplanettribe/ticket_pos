"use client";

import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";

import { PageTabs } from "@ticket-pos/ui";

import { EVENTS_TAB_KEYS, eventsTabHref, type EventsTabKey } from "@/lib/events-tabs";

/**
 * The Events list's tab strip, drawn against the path currently being read.
 *
 * A client component for the reason `SalesTabs` and `ReachTabs` are: `PageTabs`
 * needs to know which tab is current, and the only place that knows is the
 * router. The words come from the catalog here rather than from the route files
 * above, because the page they sit on is already a client component and there is
 * no server boundary between them to resolve a language at.
 *
 * All three tabs are always drawn, even when one is empty — they are a fixed
 * partition of the Organization's Events and not a set that shrinks. Bare
 * labels: the counts are #614.
 */
export function EventsTabs() {
  const t = useTranslations("events");
  const pathname = usePathname();

  const labels: Record<EventsTabKey, string> = {
    active: t("tabActive"),
    past: t("tabPast"),
    cancelled: t("tabCancelled"),
  };

  const items = EVENTS_TAB_KEYS.map((key) => ({
    href: eventsTabHref(key),
    label: labels[key],
    // Active lives at the Events list's own path, and every other Events route
    // hangs beneath it — the two sibling tabs, the create-Event page and every
    // Event's own surface. Without `exact` it would stay lit on all of them.
    exact: key === "active",
  }));

  return <PageTabs items={items} activePath={pathname} ariaLabel={t("tabsLabel")} />;
}
