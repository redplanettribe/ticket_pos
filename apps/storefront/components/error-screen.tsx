"use client";

import { readReferenceCode, reportCaughtError, type CaughtError } from "@ticket-pos/sentry-rules";
import { ErrorScreen, type ErrorScreenLabels } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore, useTransition } from "react";

import { captureFromScreen } from "@/lib/error-screen";
import { loadSentryBrowser, readPageSentryConfig, sentryBrowserRequested } from "@/lib/sentry-lazy";

/** What Next hands an error boundary: the error, and a way to render again. */
export type ErrorBoundaryProps = {
  error: CaughtError;
  reset: () => void;
};

/** An error screen's words, as the catalog's `errorScreen` namespace holds them. */
export type ErrorScreenCopy = Omit<ErrorScreenLabels, "description"> & {
  description: string;
  descriptionWithoutReference: string;
};

/** For useSyncExternalStore over a value that announces no change of its own. */
export function noSubscription() {
  return () => {};
}

/**
 * The reference code: the id of the request that rendered this page, from the
 * cookie the middleware writes. Read on every render, so a retry that fails
 * again shows the id of the retry. Nothing on the server's pass, where there is
 * no document to read; the browser's pass fills it in.
 */
function useReferenceCode(): string | null {
  return useSyncExternalStore(
    noSubscription,
    () => readReferenceCode(document.cookie),
    () => null,
  );
}

/**
 * An error screen with its words supplied (#774): reports what it caught (once,
 * and only if the server has not already, reportCaughtError), shows the
 * reference code, and retries by asking the server for the failed segment again
 * and rendering it, which a bare `reset()` would not: it re-renders from the
 * payload that already carries the error. The retry stays on the same address,
 * query string included, and touches no cookie or storage, so whatever a
 * checkout in progress keeps there is still there when the page comes back.
 *
 * The browser SDK may not have loaded yet; the screen loads it to report, and
 * with no DSN loads nothing (lib/error-screen.ts). The configuration is read on
 * the screen's first render, when the root layout's <meta> is still in the
 * document even if this screen is about to replace it, and again when it
 * reports, for a root layout the browser rendered after the server could not.
 *
 * Depends on no provider of the app's, so the root-level screen can use it in
 * place of a root layout that crashed.
 */
export function ErrorScreenBody({ error, reset, copy }: ErrorBoundaryProps & { copy: ErrorScreenCopy }) {
  const router = useRouter();
  const [retrying, startRetry] = useTransition();
  const referenceCode = useReferenceCode();
  const [writtenAtFirstRender] = useState(() =>
    typeof document === "undefined" ? null : readPageSentryConfig(document),
  );

  useEffect(() => {
    const capture = captureFromScreen({
      written: writtenAtFirstRender ?? readPageSentryConfig(document),
      requested: sentryBrowserRequested,
      load: loadSentryBrowser,
    });
    reportCaughtError(error, readReferenceCode(document.cookie), capture);
  }, [error, writtenAtFirstRender]);

  return (
    <ErrorScreen
      layout="page"
      referenceCode={referenceCode}
      retrying={retrying}
      onRetry={() =>
        startRetry(() => {
          router.refresh();
          reset();
        })
      }
      labels={{
        title: copy.title,
        description: referenceCode ? copy.description : copy.descriptionWithoutReference,
        retry: copy.retry,
        retrying: copy.retrying,
        referenceCode: copy.referenceCode,
      }}
    />
  );
}

/**
 * The route-level screen's words: rendered below the root layout, whose
 * next-intl provider is intact, so they are the page's locale.
 */
export function RouteErrorScreen(props: ErrorBoundaryProps) {
  const t = useTranslations("errorScreen");
  return (
    <ErrorScreenBody
      {...props}
      copy={{
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
