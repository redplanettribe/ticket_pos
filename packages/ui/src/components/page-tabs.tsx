import { isNavItemActive } from "../lib/nav-active";
import { cn } from "../lib/utils";

/**
 * One tab: where it points, what it is called, and whether it is the index of
 * its own surface (`exact`, see `isNavItemActive`).
 *
 * The label is a word and not a key, because this component renders what it is
 * handed: the caller holds the catalog, and @ticket-pos/ui cannot reach it
 * (ADR 0041). The shape is `SidebarNavItem`'s deliberately, so a nav module's
 * keyed entries are turned into tabs the same way they are turned into sidebar
 * rows — spread the entry, add the label.
 */
export type PageTabsItem = {
  href: string;
  label: string;
  exact?: boolean;
};

export type PageTabsProps = {
  items: readonly PageTabsItem[];
  /** The path being read, so the current tab can be marked. */
  activePath?: string;
  /** Names what the strip navigates between, for a screen reader. */
  ariaLabel: string;
  className?: string;
};

/**
 * A strip of underline tabs across the top of a surface that has more than one
 * face — the Sales surface's list and its recording tools.
 *
 * Real links in a real `nav`, not buttons and not `role="tablist"`: each tab is
 * a page with an address of its own, so it must work with the keyboard, with
 * open-in-new-tab, and with a bookmark, and the current one is announced with
 * `aria-current="page"` as every other navigation entry in this app is. The ARIA
 * tab pattern would promise arrow-key traversal of panels that are not there.
 *
 * Whether a strip is worth drawing at all — one tab is not — belongs to the
 * navigation module that produced the entries (`shouldDrawTabStrip`), not here:
 * this renders what it is handed.
 */
export function PageTabs({ items, activePath, ariaLabel, className }: PageTabsProps) {
  return (
    <nav aria-label={ariaLabel} className={cn("flex gap-6 overflow-x-auto border-b", className)}>
      {items.map((item) => {
        const active = isNavItemActive(activePath, item.href, { exact: item.exact });
        return (
          <a
            key={item.href}
            href={item.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "whitespace-nowrap border-b-2 px-1 pb-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm",
              active
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
            )}
          >
            {item.label}
          </a>
        );
      })}
    </nav>
  );
}
