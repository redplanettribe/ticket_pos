/**
 * Where a visitor is sent once they have proved they own their email address.
 *
 * This lives on its own because two unrelated paths now decide it — the sign-in
 * page reading `?next=`, and the Google Sign-In callback reading the destination
 * back out of its state cookie — and both must apply the same guard. One copy
 * means a fix to the guard cannot reach one path and miss the other.
 */

/** Where a Customer lands when nothing better was asked for: their Customer Area. */
export const DEFAULT_DESTINATION = "/tickets";

/**
 * safeNext keeps the post-sign-in destination inside this Storefront. Anything
 * that is not a plain absolute path — a full URL, or a protocol-relative "//host"
 * — is discarded, so the sign-in page can never be used to bounce a visitor to
 * another site.
 */
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith("/") || next.startsWith("//")) {
    return DEFAULT_DESTINATION;
  }
  return next;
}
