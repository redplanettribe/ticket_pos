/**
 * What the sign-in card is saying, and the one thing that can change it.
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
 * — yields the ordinary card, because the overwhelming majority of arrivals here
 * are existing Members using the door they use daily, and a query parameter must
 * never present them with a page they do not recognise.
 *
 * THIS MODULE RETURNS A TOKEN AND NOT A SENTENCE. It decides *which* card is
 * being shown; messages/en.json and messages/es.json decide what that card says
 * (#286). Nothing here imports the catalog and nothing here is handed a `t`,
 * which is what keeps this file free of React and of the i18n runtime — so the
 * fast test runner stays fast, and login-copy.test.ts asserts a decision rather
 * than prose that a copy edit would break.
 */

/** The literal value the Storefront's invitation carries. Nothing else matches. */
export const CREATE_INTENT = "create";

/**
 * Which of the two cards the sign-in page is showing.
 *
 * A closed union rather than a boolean, because a third arrival — an invitation
 * to join an Organization, say — is a third token and a third pair of catalog
 * keys, not a second flag to combine with the first.
 */
export type SignInIntent = "default" | "create";

/**
 * resolveSignInIntent maps the `intent` query parameter to the card being shown.
 *
 * The argument is typed as Next hands it over — a string, an array when the
 * parameter is repeated, or undefined — but is checked at runtime rather than
 * trusted, since a URL is whatever somebody typed. Anything that is not the
 * exact string `create` falls through to the ordinary card; that includes a
 * repeated parameter, on the grounds that `?intent=join&intent=create` is not an
 * arrival this application produced.
 */
export function resolveSignInIntent(intent: string | string[] | undefined): SignInIntent {
  return intent === CREATE_INTENT ? "create" : "default";
}
