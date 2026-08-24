"use client";

import * as React from "react";

import { cn } from "../../lib/utils";
import { CHART_SCROLL_AREA_ATTRIBUTE } from "./chart-geometry";

export type ChartScrollAreaProps = {
  /**
   * Names the scrollable region, and is the right place to say that it scrolls:
   * a reader who cannot see the scrollbar is otherwise told nothing about the
   * part of the span that is off screen.
   */
  ariaLabel: string;
  children: React.ReactNode;
  className?: string;
  /**
   * Handed the scrolling element itself, so a caller can measure it.
   *
   * Charts inside are drawn at a width in pixels rather than stretched by CSS,
   * which means somebody has to know how many pixels there are. This element is
   * the honest place to ask: it is the box the charts must fit or overflow.
   */
  ref?: React.Ref<HTMLDivElement>;
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
 * must be the scrolling ancestor rather than something further out. Their
 * hover cards find it by `CHART_SCROLL_AREA_ATTRIBUTE` to learn how much of
 * the chart is actually on screen.
 */
export function ChartScrollArea({ ariaLabel, children, className, ref }: ChartScrollAreaProps) {
  return (
    <div
      ref={ref}
      {...{ [CHART_SCROLL_AREA_ATTRIBUTE]: "" }}
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
