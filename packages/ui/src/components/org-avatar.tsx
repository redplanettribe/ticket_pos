import { Building2 } from "lucide-react";

import { cn } from "../lib/utils";

type OrgAvatarProps = {
  logoUrl?: string | null;
  name?: string;
  /**
   * Overrides the derived alt text. Defaults to "<name> logo", falling back to
   * "Organization logo" when the Organization has no name to interpolate.
   */
  alt?: string;
  className?: string;
  /**
   * "tile": a fixed 40px square cell, for list/row/card contexts where the
   * neighbouring content already assumes a square footprint.
   * "inline": a fixed-height, variable-width mark capped at 160px, for
   * header/banner contexts where a wide wordmark shouldn't be letterboxed
   * into a square. Defaults to "tile".
   */
  shape?: "tile" | "inline";
};

export function OrgAvatar({ logoUrl, name, alt: altOverride, className, shape = "tile" }: OrgAvatarProps) {
  const alt = altOverride ?? (name ? `${name} logo` : "Organization logo");

  if (logoUrl) {
    if (shape === "inline") {
      return (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={logoUrl}
          alt={alt}
          className={cn("h-8 w-auto max-w-40 shrink-0 rounded-md object-contain", className)}
        />
      );
    }

    return (
      <span className={cn("flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-muted p-1", className)}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={logoUrl} alt={alt} className="max-h-full max-w-full object-contain" />
      </span>
    );
  }

  const fallbackSize = shape === "inline" ? "h-8 w-8" : "h-10 w-10";

  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
        fallbackSize,
        className,
      )}
      aria-hidden="true"
    >
      <Building2 className="h-4 w-4" />
    </span>
  );
}
