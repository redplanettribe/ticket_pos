"use client";

import * as React from "react";

import { cn } from "../../lib/utils";

export type ChartLegendChip = {
  id: string;
  label: string;
  color: string;
};

export type ChartLegendChipsProps = {
  chips: ChartLegendChip[];
  /** The ids currently drawn. Held by the caller, above the chart(s). */
  selected: readonly string[];
  onToggle: (id: string) => void;
  /** Names what the chips filter, for a screen reader. */
  ariaLabel: string;
  className?: string;
};

/**
 * The legend, drawn as toggles rather than as a key.
 *
 * Selection deliberately lives in the caller, not here: one chip row is meant to
 * drive every chart on the surface, and a component that owned the state could
 * only ever drive itself. This renders the state and reports intent.
 *
 * A deselected chip is dimmed rather than removed, so the reader can always see
 * what they have hidden and put it back.
 */
export function ChartLegendChips({
  chips,
  selected,
  onToggle,
  ariaLabel,
  className,
}: ChartLegendChipsProps) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn("flex flex-wrap items-center gap-2", className)}
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
}
