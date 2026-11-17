import { RotateCcw, TriangleAlert } from "lucide-react";

import { cn } from "../lib/utils";
import { Logo } from "./logo";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "./ui/card";

/**
 * An error screen's words, supplied by the app: this package is shared by the
 * Staff app and the Storefront, whose catalogs are deliberately separate.
 */
export type ErrorScreenLabels = {
  title: string;
  description: string;
  retry: string;
  /** Shown on the button while the retry is under way. */
  retrying: string;
  /** The heading above the reference code. */
  referenceCode: string;
};

/**
 * How an error screen sits on the page: "page" stands alone, centred on the
 * screen with the logo, for when nothing of the app's chrome is left around it;
 * "inline" sits in a page's content area, inside chrome that is still drawn.
 */
export type ErrorScreenLayout = "page" | "inline";

type ErrorScreenProps = {
  labels: ErrorScreenLabels;
  /**
   * The code a person quotes to support; the block is left out when there is
   * none rather than showing an empty one.
   */
  referenceCode: string | null;
  onRetry: () => void;
  retrying?: boolean;
  layout: ErrorScreenLayout;
};

/**
 * What a person sees in place of a page that crashed: what happened, a way to
 * try again, and a reference code to quote. Presentational only; catching and
 * reporting the error is the app's.
 */
export function ErrorScreen({
  labels,
  referenceCode,
  onRetry,
  retrying = false,
  layout,
}: ErrorScreenProps) {
  const card = (
    <Card role="alert" className={cn("w-full max-w-md", layout === "inline" && "mx-auto")}>
      <CardHeader>
        <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-destructive/10 text-destructive">
          <TriangleAlert className="size-5" aria-hidden="true" />
        </div>
        <CardTitle className="text-xl text-balance">{labels.title}</CardTitle>
        <CardDescription>{labels.description}</CardDescription>
      </CardHeader>
      {referenceCode ? (
        <CardContent>
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{labels.referenceCode}</p>
          <code className="mt-1 block select-all break-all rounded-md bg-muted px-3 py-2 font-mono text-xs sm:text-sm">
            {referenceCode}
          </code>
        </CardContent>
      ) : null}
      <CardFooter>
        <Button type="button" onClick={onRetry} disabled={retrying} aria-busy={retrying} className="w-full sm:w-auto">
          <RotateCcw aria-hidden="true" className={cn(retrying && "animate-spin")} />
          {retrying ? labels.retrying : labels.retry}
        </Button>
      </CardFooter>
    </Card>
  );

  if (layout === "inline") {
    return <div className="py-8">{card}</div>;
  }
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-muted/40 p-4">
      <Logo withWordmark className="mb-6 text-lg text-primary" markClassName="size-7" />
      {card}
    </main>
  );
}
