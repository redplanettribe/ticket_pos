import { cn } from "../lib/utils";

type PlatformMarkProps = {
  /**
   * "tile": a fixed 40px square cell, matching `OrgAvatar`'s tile so the
   * switcher's Platform entry lines up with the Organizations above it.
   * "inline": the 32px square `OrgAvatar` falls back to in a panel header.
   */
  shape?: "tile" | "inline";
  className?: string;
};

/**
 * The platform's own mark, standing where an Organization's logo stands (#192).
 *
 * Deliberately not an Organization avatar and deliberately not the product
 * Logo: operator authority is orthogonal to Membership (CONTEXT.md), so the
 * thing an operator is acting as needs a face of its own. One definition, worn
 * both by the switcher's Platform entry and by the Operator Dashboard's panel
 * header, so crossing over shows the same mark that was clicked.
 */
export function PlatformMark({ shape = "tile", className }: PlatformMarkProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex shrink-0 items-center justify-center rounded-md bg-primary/10 text-sm font-semibold text-primary",
        shape === "inline" ? "size-8" : "size-10",
        className,
      )}
    >
      P
    </span>
  );
}
