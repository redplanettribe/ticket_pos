"use client";

import { usePathname } from "next/navigation";

import { PageTabs, type PageTabsItem } from "@ticket-pos/ui";

type SalesTabsProps = {
  /** The tabs to draw, already in the reader's language (see the Sales layout). */
  items: PageTabsItem[];
  ariaLabel: string;
};

/**
 * The Sales sub-tabs, drawn against the path currently being read.
 *
 * A client component for one reason: `PageTabs` needs to know which tab is
 * current, and the only place that knows is the router. The words and the shape
 * of the strip are decided on the server and handed down, the way
 * `EventShellClient` does for the Event panel.
 */
export function SalesTabs({ items, ariaLabel }: SalesTabsProps) {
  const pathname = usePathname();

  return <PageTabs items={items} activePath={pathname} ariaLabel={ariaLabel} />;
}
