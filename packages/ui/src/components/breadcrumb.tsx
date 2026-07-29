import { Fragment } from "react";
import { ChevronRight } from "lucide-react";

import { cn } from "../lib/utils";

export type BreadcrumbItem = {
  label: string;
  /** When omitted, the item renders as the non-interactive terminal (current-page) crumb. */
  href?: string;
};

type BreadcrumbProps = {
  items: BreadcrumbItem[];
  className?: string;
  /** Defaults to the English "Breadcrumb", so omitting it changes nothing. */
  label?: string;
};

export function Breadcrumb({ items, className, label = "Breadcrumb" }: BreadcrumbProps) {
  return (
    <nav aria-label={label} className={cn("min-w-0", className)}>
      <ol className="flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
        {items.map((item, index) => {
          const isLast = index === items.length - 1;
          return (
            <Fragment key={`${item.label}-${index}`}>
              <li className={cn("flex items-center", isLast && "min-w-0")}>
                {item.href ? (
                  <a
                    href={item.href}
                    className="shrink-0 underline-offset-4 transition-colors hover:text-foreground hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded"
                  >
                    {item.label}
                  </a>
                ) : (
                  <span aria-current="page" className="truncate font-medium text-foreground">
                    {item.label}
                  </span>
                )}
              </li>
              {!isLast ? (
                <li aria-hidden="true" className="shrink-0">
                  <ChevronRight className="h-4 w-4" />
                </li>
              ) : null}
            </Fragment>
          );
        })}
      </ol>
    </nav>
  );
}
