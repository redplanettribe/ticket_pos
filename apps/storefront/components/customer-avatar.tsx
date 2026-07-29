import { cn } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

/**
 * The Customer's Avatar: their photo when they have one, their initials when
 * they don't. The circle renders wherever the signed-in Customer is represented
 * — the header chip and "My info" — and the fallback is why an Avatar is never
 * required: everyone has initials.
 *
 * The image is object-cover in a circle, deliberately without a cropping UI: an
 * off-centre photo is the Customer's to re-upload, not a tool this page grows.
 */

type CustomerAvatarProps = {
  avatarUrl: string | null;
  firstName: string;
  lastName: string;
  email: string;
  className?: string;
};

/**
 * initials prefers the name's first letters and falls back to the email's
 * first character — a Customer who signed in by passcode and never bought
 * anything has no name yet, but always has an email.
 */
export function customerInitials(firstName: string, lastName: string, email: string): string {
  const first = firstName.trim();
  const last = lastName.trim();
  const fromName = `${first.charAt(0)}${last.charAt(0)}`.toUpperCase();
  if (fromName !== "") return fromName;
  return email.trim().charAt(0).toUpperCase();
}

export function CustomerAvatar({
  avatarUrl,
  firstName,
  lastName,
  email,
  className,
}: CustomerAvatarProps) {
  // Alt text is copy: it is what a screen reader says out loud, and it belongs
  // to the Customer's own details like everything else about their photo.
  const t = useTranslations("myInfo");

  if (avatarUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={avatarUrl}
        alt={t("photoAlt")}
        className={cn("h-8 w-8 shrink-0 rounded-full object-cover", className)}
      />
    );
  }

  return (
    <span
      className={cn(
        "flex h-8 w-8 shrink-0 select-none items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground",
        className,
      )}
      aria-hidden="true"
    >
      {customerInitials(firstName, lastName, email)}
    </span>
  );
}
