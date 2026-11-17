"use client";

import { RouteErrorScreen, type ErrorBoundaryProps } from "@/components/error-screen";

/**
 * The route-level error screen (#774): a page under a locale crashed. It renders
 * inside the root layout, whose next-intl provider is intact, so it speaks the
 * page's language, and in place of the page, which draws its own shell
 * (StorefrontShell), so it stands alone with the logo. A crash in the root
 * layout itself is the root-level screen's (app/global-error.tsx).
 */
export default function RouteError(props: ErrorBoundaryProps) {
  return <RouteErrorScreen {...props} />;
}
