"use client";

import { useEffect, useState } from "react";

/**
 * The element's current inner width, tracked as it changes.
 *
 * The charts are drawn at a width in pixels rather than stretched by CSS, so
 * something has to say how many pixels are going spare, and that answer moves
 * when the window resizes or the sidebar collapses. `clientWidth` rather than
 * the observer's `contentRect`, because it is the scrollable box's own inner
 * width and so already excludes any scrollbar the charts themselves provoked.
 *
 * Zero until the element is measured, which is the server-rendered pass and the
 * first client frame; the caller reads that as "the span decides alone".
 */
export function useElementWidth(element: HTMLElement | null): number {
  const [width, setWidth] = useState(0);
  useEffect(() => {
    if (!element) {
      return;
    }
    const measure = () => setWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [element]);
  return width;
}
