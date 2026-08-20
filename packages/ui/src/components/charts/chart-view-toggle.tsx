"use client";

import * as React from "react";

import { cn } from "../../lib/utils";

export type ChartViewOption<Id extends string = string> = {
  id: Id;
  label: string;
};

export type ChartViewToggleProps<Id extends string = string> = {
  options: readonly ChartViewOption<Id>[];
  value: Id;
  onChange: (id: Id) => void;
  /** Names what the toggle chooses between, for a screen reader. */
  ariaLabel: string;
  className?: string;
};

/**
 * A segmented control for choosing which of several ways a surface's charts are
 * drawn — Sales Trends picks between its Daily and Cumulative views with one.
 *
 * The choice lives in the caller, like `ChartLegendChips`' selection does, and
 * for the same reason: one control is meant to move every chart on the surface,
 * and a component that owned the state could only move itself.
 *
 * Built on real radio inputs rather than on buttons carrying `aria-pressed`,
 * which is the shape the chips use. The difference is the one that matters here:
 * chips are independent switches, and this is a single choice between mutually
 * exclusive options. Native radios say so to assistive technology, and give
 * arrow-key navigation within the group and a single tab stop for it — behaviour
 * worth having for free rather than reimplementing on buttons.
 */
export function ChartViewToggle<Id extends string = string>({
  options,
  value,
  onChange,
  ariaLabel,
  className,
}: ChartViewToggleProps<Id>) {
  // Radios group by shared `name`, so two toggles on one page must not share
  // one. Generated rather than asked of the caller, which would make correctness
  // depend on somebody remembering.
  const name = React.useId();
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className={cn("inline-flex rounded-full border p-0.5 text-xs font-medium", className)}
    >
      {options.map((option) => {
        const isSelected = option.id === value;
        return (
          <label
            key={option.id}
            className={cn(
              "cursor-pointer rounded-full px-3 py-1 transition-colors",
              // The focus ring is drawn on the label, because the input it
              // belongs to is invisible: without this the keyboard user moving
              // through the group would see nothing move.
              "has-[:focus-visible]:outline-none has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring has-[:focus-visible]:ring-offset-2",
              isSelected
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <input
              type="radio"
              name={name}
              value={option.id}
              checked={isSelected}
              onChange={() => onChange(option.id)}
              className="sr-only"
            />
            {option.label}
          </label>
        );
      })}
    </div>
  );
}
