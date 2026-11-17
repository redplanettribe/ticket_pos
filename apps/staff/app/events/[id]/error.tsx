"use client";

import { RouteErrorScreen, type ErrorBoundaryProps } from "../../error-screen";

/**
 * A crash in one of an Event's pages, shown inside the Event's panel: the
 * layout above this segment drew it and it is still standing (#772).
 */
export default function EventError(props: ErrorBoundaryProps) {
  return <RouteErrorScreen {...props} layout="inline" />;
}
