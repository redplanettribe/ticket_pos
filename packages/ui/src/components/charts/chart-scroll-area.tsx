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
  /**
   * Opens the window scrolled to its right-hand end, the newest days, rather
   * than at the oldest: a span longer than the card is drawn oldest first, and
   * the reader almost always came to see the latest days.
   *
   * `key` names the data drawn, and the window anchors once per key and then
   * leaves the reader alone: a hover, a resize or a re-render with the same key
   * must not drag them back from the days they scrolled to. Give it a new key
   * exactly when what is drawn changes (another span, comparison or view).
   *
   * `contentWidth` is the width the charts are drawn at, and a reason to look
   * again rather than a value read: charts are measured over the frames after
   * mount, so the content only becomes wider than the window once that width
   * settles. Until it does there is no overflow, and the anchor stays untaken.
   */
  openAtNewestEnd?: { key: string; contentWidth: number };
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
export function ChartScrollArea({ ariaLabel, children, className, ref, openAtNewestEnd }: ChartScrollAreaProps) {
  const element = React.useRef<HTMLDivElement | null>(null);
  const setElement = React.useCallback(
    (node: HTMLDivElement | null) => {
      element.current = node;
      if (typeof ref === "function") {
        ref(node);
      } else if (ref) {
        ref.current = node;
      }
    },
    [ref],
  );
  useOpenedAtNewestEnd(element, openAtNewestEnd?.key, openAtNewestEnd?.contentWidth);
  return (
    <div
      ref={setElement}
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

/**
 * Sets the window's offset to its newest end once per key (see
 * `openAtNewestEnd`), and does nothing without a key.
 *
 * It sets no state, so it cannot loop: it runs again only when the key or the
 * content width changes, and once the anchor is taken it returns at once.
 *
 * A new key is a fresh anchor to take, so the flag is cleared here rather than
 * compared against the key. Comparing would look equivalent and is not: a span
 * that fits its card never takes its anchor, and the next key must still find
 * its own anchor untaken.
 *
 * A layout effect, so the offset is set before the browser paints. In an
 * ordinary effect the reader sees the oldest days for a frame and then a jump.
 */
function useOpenedAtNewestEnd(
  element: React.RefObject<HTMLDivElement | null>,
  key: string | undefined,
  contentWidth: number | undefined,
): void {
  const anchor = React.useRef<{ key: string | undefined; taken: boolean }>({ key: undefined, taken: false });
  React.useLayoutEffect(() => {
    if (anchor.current.key !== key) {
      anchor.current = { key, taken: false };
    }
    const area = element.current;
    if (key === undefined || area === null || anchor.current.taken) {
      return;
    }
    const overflow = area.scrollWidth - area.clientWidth;
    if (overflow <= 0) {
      // Either the content fits, so there is no end to open on, or nothing has
      // been laid out yet. Neither is worth remembering: a later frame may
      // still take the anchor.
      return;
    }
    area.scrollLeft = overflow;
    anchor.current.taken = true;
  }, [element, key, contentWidth]);
}
