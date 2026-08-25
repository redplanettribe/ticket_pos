"use client";

import { ChevronDown } from "lucide-react";
import * as React from "react";

import { cn } from "../../lib/utils";

export type ChartLegendChip = {
  id: string;
  label: string;
  color: string;
};

/**
 * The words on the Show-all button. Passed in because this package has no
 * i18n: the app that owns the strings owns the count's grammar too.
 */
export type ChartLegendChipsCollapseLabels = {
  /** "Show all N", where N is every chip listed, drawn and dimmed alike. */
  showAll: (count: number) => string;
  showFewer: string;
};

export type ChartLegendChipsProps = {
  chips: ChartLegendChip[];
  /** The ids currently drawn. Held by the caller, above the chart(s). */
  selected: readonly string[];
  onToggle: (id: string) => void;
  /** Names what the chips filter, for a screen reader. */
  ariaLabel: string;
  className?: string;
  /**
   * Opt in to opening clamped to three chip rows, with a "Show all N" button
   * whenever the chips overflow the clamp. Off by default, and when off the
   * legend is exactly the plain wrapping row it always was.
   */
  collapsible?: ChartLegendChipsCollapseLabels;
};

/** How many chip rows a collapsed legend shows. */
const COLLAPSED_ROWS = 3;

/**
 * The clamp, derived from the chip's own box rather than measured: a chip is
 * `text-xs` (1rem line height) plus `py-1` (0.25rem each side) plus a 1px
 * border each side, and rows sit `gap-2` (0.5rem) apart. Stated as a calc so
 * the clamp answers to the root font size the way the chips do.
 */
const COLLAPSED_MAX_HEIGHT = `calc(${COLLAPSED_ROWS} * (1rem + 2 * 0.25rem + 2px) + ${COLLAPSED_ROWS - 1} * 0.5rem)`;

/**
 * The legend, drawn as toggles rather than as a key.
 *
 * Selection deliberately lives in the caller, not here: one chip row is meant to
 * drive every chart on the surface, and a component that owned the state could
 * only ever drive itself. This renders the state and reports intent.
 *
 * A deselected chip is dimmed rather than removed, so the reader can always see
 * what they have hidden and put it back.
 *
 * With `collapsible` set, the legend opens clamped to three rows and offers
 * the rest behind a "Show all N" button — but only when there is a rest: the
 * button exists exactly while the chips overflow the clamp, and goes away when
 * a wider window lets them all fit. Whether it is open is this component's
 * own state, so every mount opens collapsed and the caller keeps no record.
 */
export function ChartLegendChips({
  chips,
  selected,
  onToggle,
  ariaLabel,
  className,
  collapsible,
}: ChartLegendChipsProps) {
  const groupId = React.useId();
  const [group, setGroup] = React.useState<HTMLDivElement | null>(null);
  const [expanded, setExpanded] = React.useState(false);
  const overflowing = useOverflowsClamp(collapsible ? group : null, expanded, chips);

  // A resize that fits everything takes the button away; if that happens
  // while open, the legend closes so the next narrowing meets a clamp, not an
  // unclamped legend with no way to fold it.
  React.useEffect(() => {
    if (!overflowing) {
      setExpanded(false);
    }
  }, [overflowing]);

  const list = (
    <div
      role="group"
      aria-label={ariaLabel}
      {...(collapsible
        ? {
            id: groupId,
            ref: setGroup,
            style: expanded ? undefined : { maxHeight: COLLAPSED_MAX_HEIGHT },
          }
        : {})}
      className={cn(
        "flex flex-wrap items-center gap-2",
        collapsible && "overflow-hidden",
        className,
      )}
    >
      {chips.map((chip) => {
        const isSelected = selected.includes(chip.id);
        return (
          <button
            key={chip.id}
            type="button"
            aria-pressed={isSelected}
            onClick={() => onToggle(chip.id)}
            className={cn(
              "inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              isSelected
                ? "border-transparent bg-secondary text-secondary-foreground"
                : "border-dashed text-muted-foreground hover:text-foreground",
            )}
          >
            <span
              aria-hidden
              className="size-2 shrink-0 rounded-full"
              style={{ backgroundColor: isSelected ? chip.color : "transparent", boxShadow: `inset 0 0 0 1px ${chip.color}` }}
            />
            {chip.label}
          </button>
        );
      })}
    </div>
  );

  if (!collapsible) {
    return list;
  }

  return (
    <div className="flex min-w-0 flex-col items-start gap-2">
      {list}
      {overflowing ? (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={groupId}
          onClick={() => setExpanded((open) => !open)}
          className={cn(
            "inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
          )}
        >
          {expanded ? collapsible.showFewer : collapsible.showAll(chips.length)}
          <ChevronDown
            aria-hidden
            className={cn("size-3.5 shrink-0 transition-transform", expanded && "rotate-180")}
          />
        </button>
      ) : null}
    </div>
  );
}

/**
 * Whether the chips need more room than the clamp gives them, kept current as
 * the element resizes and as the chip list changes shape.
 *
 * While collapsed the answer is the element's own: content taller than its
 * clamped box. While open there is no clamp to compare against, so the clamp's
 * height is remembered from the last collapsed measurement — it was overflowing
 * then, or there would have been no button to open it with — and the content
 * is measured against that. Reads only; nothing here writes a size back, so a
 * resize cannot chase its own tail (cf. the hover card in PR #418).
 */
function useOverflowsClamp(
  element: HTMLElement | null,
  expanded: boolean,
  chips: readonly ChartLegendChip[],
): boolean {
  const [overflowing, setOverflowing] = React.useState(false);
  const clampHeight = React.useRef(0);
  React.useEffect(() => {
    if (!element) {
      setOverflowing(false);
      return;
    }
    const measure = () => {
      if (!expanded) {
        clampHeight.current = element.clientHeight;
      }
      const limit = expanded ? clampHeight.current : element.clientHeight;
      setOverflowing(element.scrollHeight > limit);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
    // The chip list is a dependency because a collapsed legend that stays
    // exactly at its clamp while its chips change does not resize, and so
    // would not otherwise be re-measured.
  }, [element, expanded, chips]);
  return overflowing;
}
