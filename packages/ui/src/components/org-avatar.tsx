import { Building2 } from "lucide-react";

import { cn } from "../lib/utils";

type OrgAvatarProps = {
  logoUrl?: string | null;
  name?: string;
  className?: string;
};

export function OrgAvatar({ logoUrl, name, className }: OrgAvatarProps) {
  if (logoUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={logoUrl}
        alt={name ? `${name} logo` : "Organization logo"}
        className={cn("h-8 w-8 shrink-0 rounded-md object-contain", className)}
      />
    );
  }

  return (
    <span
      className={cn(
        "flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
        className,
      )}
      aria-hidden="true"
    >
      <Building2 className="h-4 w-4" />
    </span>
  );
}
