/**
 * What the sign-in card says, and the one thing that can change it.
 *
 * The Storefront carries a "Create an event" invitation that leaves that app for
 * `<staff origin>/login?intent=create` (issue #259). A stranger arriving that way
 * is a would-be Organizer who has never seen this application, and a bare
 * "Sign in" leaves them guessing whether the box is meant for people who already
 * have an account. So the parameter selects the card's title and description,
 * and nothing else anywhere: not authentication, not the auth fork's routing,
 * not middleware. `/login` is a public path, which is why the parameter survives
 * at all — middleware returns before it clears any query.
 *
 * The rule that matters is the negative one. Every value other than the exact
 * create intent — absent, unknown, empty, repeated, mangled, not a string at all
 * — yields today's copy byte-for-byte, because the overwhelming majority of
 * arrivals here are existing Members using the door they use daily, and a query
 * parameter must never present them with a page they do not recognise.
 *
 * Hardcoded English: the staff app has no i18n (ADR 0033), and a Spanish reader
 * crossing from the Storefront meets an English application. That is a known,
 * accepted cliff, not an omission here.
 */

/** The literal value the Storefront's invitation carries. Nothing else matches. */
export const CREATE_INTENT = "create";

/** The title and description of the sign-in card, in its email step. */
export type SignInCopy = {
  title: string;
  description: string;
};

/**
 * The copy the card has shown since it existed. Any change to these two strings
 * changes the page every Member sees, so login-copy.test.ts holds its own copy
 * of them and fails on an edit.
 */
export const DEFAULT_SIGN_IN_COPY: SignInCopy = {
  title: "Sign in",
  description: "Enter your email to receive a one-time passcode.",
};

/**
 * What a would-be Organizer is told instead: that they are in the right place,
 * and what the next two steps are. Signing in comes first, the organization
 * next, the published event after that (issue #262).
 */
export const CREATE_INTENT_SIGN_IN_COPY: SignInCopy = {
  title: "Create your event",
  description:
    "Sign in with your email to get started — next you'll set up your organization, and then you can create and publish your event.",
};

/**
 * resolveSignInCopy maps the `intent` query parameter to the card's copy.
 *
 * The argument is typed as Next hands it over — a string, an array when the
 * parameter is repeated, or undefined — but is checked at runtime rather than
 * trusted, since a URL is whatever somebody typed. Anything that is not the
 * exact string `create` falls through to today's copy; that includes a repeated
 * parameter, on the grounds that `?intent=join&intent=create` is not an arrival
 * this application produced.
 *
 * A fresh object is returned each call so the exported constants cannot be
 * mutated through a caller.
 */
export function resolveSignInCopy(intent: string | string[] | undefined): SignInCopy {
  if (intent === CREATE_INTENT) {
    return { ...CREATE_INTENT_SIGN_IN_COPY };
  }
  return { ...DEFAULT_SIGN_IN_COPY };
}
