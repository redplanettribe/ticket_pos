import { ArrowLeft, SearchX } from "lucide-react";
import { cloneElement, type ReactElement, type ReactNode } from "react";

import { Logo } from "./logo";
import { Button } from "./ui/button";
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from "./ui/card";

/**
 * A not-found screen's words, supplied by the app: this package is shared by
 * the Staff app and the Storefront, whose catalogs are deliberately separate.
 */
export type NotFoundScreenLabels = {
  title: string;
  description: string;
};

type NotFoundScreenProps = {
  labels: NotFoundScreenLabels;
  /**
   * The way back, as the app's own link element (each app routes its links its
   * own way: the Storefront's prefix the address's locale). Its text is the
   * app's too. It is drawn as the card's button.
   */
  backLink: ReactElement<{ children?: ReactNode }>;
};

/**
 * What a person sees in place of an address that leads nowhere (#793): that
 * there is nothing here, and a way back. It stands alone on the screen with
 * the logo, the way the error screen's "page" layout does, and shares its look.
 * Unlike the error screen it has no retry and no reference code: nothing
 * failed, so there is nothing to try again or to report.
 *
 * Presentational only; answering 404 and choosing the words are the app's.
 */
export function NotFoundScreen({ labels, backLink }: NotFoundScreenProps) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-muted/40 p-4">
      <Logo withWordmark className="mb-6 text-lg text-primary" markClassName="size-7" />
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
            <SearchX className="size-5" aria-hidden="true" />
          </div>
          <CardTitle className="text-xl text-balance">{labels.title}</CardTitle>
          <CardDescription>{labels.description}</CardDescription>
        </CardHeader>
        <CardFooter>
          <Button asChild className="w-full sm:w-auto">
            {withBackIcon(backLink)}
          </Button>
        </CardFooter>
      </Card>
    </main>
  );
}

/** The link with the arrow drawn ahead of its own text. */
function withBackIcon(link: ReactElement<{ children?: ReactNode }>) {
  return cloneElement(
    link,
    undefined,
    <ArrowLeft aria-hidden="true" />,
    link.props.children,
  );
}
