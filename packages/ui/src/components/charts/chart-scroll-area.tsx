"use client";

import * as React from "react";

import { cn } from "../../lib/utils";

export type ChartScrollAreaProps = {
  /**
   * Names the scrollable region, and is the right place to say that it scrolls:
   * a reader who cannot see the scrollbar is otherwise told nothing about the
   * part of the span that is off screen.
   */
  ariaLabel: string;
  children: React.ReactNode;
  className?: string;
};

/**
 * The one horizontally scrolling window every chart on a surface is drawn in.
 *
 * A surface built from several charts over the same X axis — Sales Trends draws
 * tickets above Takings over the same days — only holds together if the same
 * bucket sits at the same horizontal position in all of them. Putting every
 * chart in one scrolling element makes that structural: there is a single scroll
 * offset because there is a single thing that scrolls. The alternative, a scroll
 * container per chart with listeners copying an offset between them, is the same
 * arrangement with a way to drift added — and every way it drifts breaks the
 * comparison the surface exists to make.
 *
 * `tabIndex` is what makes the region reachable without a mouse: a focused
 * scrollable box scrolls with the arrow keys, Home and End for free, but only if
 * it can be focused at all. Trackpads and shift-wheel scroll it natively.
 *
 * Charts inside pin their axes to this element with `position: sticky`, so this
 * must be the scrolling ancestor rather than something further out.
 */
export function ChartScrollArea({ ariaLabel, children, className }: ChartScrollAreaProps) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      tabIndex={0}
      className={cn(
        "overflow-x-auto overscroll-x-contain rounded-sm",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        className,
      )}
    >
      {children}
    </div>
  );
}
