"use client";

import * as Sentry from "@sentry/nextjs";
import { ErrorScreen, type ErrorScreenLabels } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore, useTransition } from "react";

import { reportCaughtError } from "@/lib/error-screen";
import { readReferenceCode } from "@/lib/request-id";

/** What Next hands an error boundary: the error, and a way to render again. */
export type ErrorBoundaryProps = {
  error: Error & { digest?: string };
  reset: () => void;
};

function noSubscription() {
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
  layout: "page" | "inline";
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
export function RouteErrorScreen({ layout, ...props }: ErrorBoundaryProps & { layout: "page" | "inline" }) {
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
