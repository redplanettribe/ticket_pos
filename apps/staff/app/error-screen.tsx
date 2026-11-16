"use client";

import * as Sentry from "@sentry/nextjs";
import { ErrorScreen, type ErrorScreenLabels, type ErrorScreenLayout } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore, useTransition } from "react";

import { reportCaughtError, type CaughtError } from "@/lib/error-screen";
import { readReferenceCode } from "@/lib/request-id";
import { startBrowserSentry } from "@/lib/sentry-browser";

/** What Next hands an error boundary: the error, and a way to render again. */
export type ErrorBoundaryProps = {
  error: CaughtError;
  reset: () => void;
};

/** For useSyncExternalStore over a value that announces no change of its own. */
export function noSubscription() {
  return () => {};
}

/**
 * The reference code: the id of the request that rendered this page, from the
 * cookie the middleware writes (lib/request-id.ts). Read on every render, so a
 * retry that fails again shows the id of the retry. Nothing on the server's
 * pass, where there is no document to read; the browser's pass fills it in.
 */
export function useReferenceCode(): string | null {
  return useSyncExternalStore(
    noSubscription,
    () => readReferenceCode(document.cookie),
    () => null,
  );
}

function captureInBrowser(error: unknown, requestId: string | null) {
  // Not started yet when the page came as Next's bare error document, which has
  // no Sentry meta (lib/sentry-browser.ts); dormant with no DSN either way.
  startBrowserSentry();
  Sentry.captureException(error, requestId ? { tags: { request_id: requestId } } : undefined);
}

/**
 * An error screen with its words supplied: reports what it caught (once, and
 * only if the server has not already, lib/error-screen.ts), shows the reference
 * code, and retries by asking the server for the failed segment again and
 * rendering it, which a bare `reset()` would not: it re-renders from the payload
 * that already carries the error.
 *
 * Depends on no provider of the app's, so the root-level screen can use it in
 * place of a root layout that crashed.
 */
export function ErrorScreenBody({
  error,
  reset,
  labels,
  layout,
}: ErrorBoundaryProps & {
  labels: Omit<ErrorScreenLabels, "description"> & { description: string; descriptionWithoutReference: string };
  layout: ErrorScreenLayout;
}) {
  const router = useRouter();
  const [retrying, startRetry] = useTransition();
  const referenceCode = useReferenceCode();

  useEffect(() => {
    reportCaughtError(error, readReferenceCode(document.cookie), captureInBrowser);
  }, [error]);

  return (
    <ErrorScreen
      layout={layout}
      referenceCode={referenceCode}
      retrying={retrying}
      onRetry={() =>
        startRetry(() => {
          router.refresh();
          reset();
        })
      }
      labels={{
        title: labels.title,
        description: referenceCode ? labels.description : labels.descriptionWithoutReference,
        retry: labels.retry,
        retrying: labels.retrying,
        referenceCode: labels.referenceCode,
      }}
    />
  );
}

/**
 * The route-level screen: rendered below the root layout, so next-intl's
 * provider is there and the words follow the Staff Locale.
 */
export function RouteErrorScreen({ layout, ...props }: ErrorBoundaryProps & { layout: ErrorScreenLayout }) {
  const t = useTranslations("errorScreen");
  return (
    <ErrorScreenBody
      {...props}
      layout={layout}
      labels={{
        title: t("title"),
        description: t("description"),
        descriptionWithoutReference: t("descriptionWithoutReference"),
        retry: t("retry"),
        retrying: t("retrying"),
        referenceCode: t("referenceCode"),
      }}
    />
  );
}
