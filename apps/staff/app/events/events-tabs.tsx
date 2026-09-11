"use client";

import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";

import { PageTabs } from "@ticket-pos/ui";

import { EVENTS_TAB_KEYS, eventsTabHref, type EventsTabKey } from "@/lib/events-tabs";

type EventsTabsProps = {
  /**
   * How many Events each tab holds, or null while the payload is still on the
   * wire (#614).
   *
   * Required and nullable rather than optional, so that the one caller has to
   * say which of the two it means: a caller that simply forgot the counts would
   * otherwise render a bare strip and no compiler would ask why.
   *
   * null, and deliberately not three zeros. A count is a claim about the
   * Organization's Events, and before the list arrives there is no such claim
   * to make: "Cancelled (0)" would assert the one thing the counts exist to say
   * at the one moment nobody can say it. So the labels stay bare for exactly as
   * long as the load does, and each count appears in the same render as the
   * rows it counts — no placeholder, no skeleton, no spinner of its own.
   */
  counts: Record<EventsTabKey, number> | null;
};

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
 * partition of the Organization's Events and not a set that shrinks — and each
 * carries its count (#614), which no other tab strip in this app does. That is a
 * deliberate deviation and not an oversight: Cancelled is a tab a reader would
 * otherwise have to open to learn it was worth opening. The strip is otherwise
 * its siblings' twin.
 */
export function EventsTabs({ counts }: EventsTabsProps) {
  const t = useTranslations("events");
  const pathname = usePathname();

  const labels: Record<EventsTabKey, string> = {
    active: t("tabActive"),
    past: t("tabPast"),
    cancelled: t("tabCancelled"),
  };

  const items = EVENTS_TAB_KEYS.map((key) => ({
    href: eventsTabHref(key),
    // The count rides inside the label, through the catalog, because `PageTabs`
    // takes a label and must not grow a `count` field for this one caller: the
    // Sales and Reach strips would gain a prop neither of them has any use for,
    // which is how a shared component rots. Composing it in the catalog rather
    // than in JSX also leaves the marks around the number — the brackets, the
    // spacing — to the language, instead of hard-coding English punctuation
    // around a translated word.
    label: counts
      ? t("tabCount", { label: labels[key], count: counts[key] })
      : labels[key],
    // Active lives at the Events list's own path, and every other Events route
    // hangs beneath it — the two sibling tabs, the create-Event page and every
    // Event's own surface. Without `exact` it would stay lit on all of them.
    exact: key === "active",
  }));

  return <PageTabs items={items} activePath={pathname} ariaLabel={t("tabsLabel")} />;
}
