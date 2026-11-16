"use client";

import { RouteErrorScreen, type ErrorBoundaryProps } from "./error-screen";

/**
 * The route-level error screen (#772): a page that crashed before its own shell
 * was drawn. It renders inside the root layout, whose providers and Staff Locale
 * are intact, and stands alone on the screen because the panels around a staff
 * page are drawn by the page itself (StaffPageShell). A crash inside a page's
 * content, under a shell that did render, is caught there instead
 * (ContentErrorBoundary) and shown inside it; an Event's pages have their own,
 * inside the Event's panel.
 */
export default function RouteError(props: ErrorBoundaryProps) {
  return <RouteErrorScreen {...props} layout="page" />;
}
