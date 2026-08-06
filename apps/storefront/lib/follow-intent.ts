/**
 * The Follow somebody asked for before they could be asked who they are (#219).
 *
 * The Follow control is drawn for anonymous visitors, on surfaces whose traffic
 * is overwhelmingly anonymous. Pressing it while signed out therefore has to
 * survive a mailbox, a passcode, possibly a trip through Google, and a redirect
 * back — and it does that as ONE STRING travelling in the open: a `follow` query
 * parameter on the sign-in address, relayed as a `follow` field on the verify
 * request, acted on by the API.
 *
 * In the open, and deliberately not in browser storage. A value the server never
 * sees is a value the server cannot validate; this one is validated on both
 * sides, and the guard below is the same rule the API applies
 * (backend/internal/customers/service/followintent.go). The two are stated twice
 * rather than shared because they answer different questions: the API's decides
 * whether to refuse a request, and this one decides whether a link anybody can
 * craft is allowed to put a value into this app's own sign-in flow.
 *
 * What an intent CANNOT say is the point of the format. It names a subject — a
 * kind and an identifier — and never a subscriber: there is no email in it and
 * no way to spell one. Whose Follow it becomes is decided by the Customer
 * Session verification mints, on the far side, and by nothing a browser can put
 * in a URL. And it is not a destination: where the visitor lands is the existing
 * `next`, guarded by `safeNext`, so an intent can steer nobody anywhere.
 */

// The ".ts" is written out because the unit tests run this module directly under
// `node --experimental-strip-types`, which resolves specifiers exactly. Next
// resolves it identically.
import { pathWithQuery } from "./current-path.ts";
import { SIGN_IN_PATH, safeNext } from "./destination.ts";

/**
 * The kinds of thing a Follow may be about, as the wire spells them.
 *
 * A closed set matched exactly, so an unrecognised kind is refused rather than
 * guessed at. Tag Follows (#218) add "tag" here and change nothing else in this
 * file — which is the whole reason the control and this module are written in
 * terms of a kind and a key rather than in terms of Organizations.
 */
const FOLLOW_SUBJECT_KINDS = ["organization", "tag"] as const;

export type FollowSubjectKind = (typeof FOLLOW_SUBJECT_KINDS)[number];

/** The one character between the kind and the key, and never inside either. */
const SEPARATOR = ":";

/**
 * The shape an identifier must have: the slug pattern this platform issues.
 *
 * It admits no "/", ":", ".", "@" or whitespace, which is what makes a URL, a
 * protocol-relative host, a path traversal and an email address unspellable in
 * the subject position.
 */
const KEY_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/**
 * The shape a TAG identifier must have, which is not a slug.
 *
 * A Tag's canonical key is its display name lowercased with whitespace collapsed
 * (ADR 0004), so it carries interior spaces — and two of the twelve seeded
 * Preset Tags, "arts & theatre" and "food & drink", carry an ampersand. Checked
 * against the slug pattern above, those two would be dropped silently: the
 * control renders, the visitor signs in, and the Follow they asked for is not
 * there.
 *
 * Loosened exactly as far as the data requires. Separators appear only BETWEEN
 * runs of letters and digits, never leading or trailing, and they may run
 * together — "arts & theatre" is a space, an ampersand and a space in a row.
 * "/", ":", ".", "@" stay unspellable, which is the property the strict pattern
 * was there for.
 */
const TAG_KEY_PATTERN = /^[a-z0-9]+(?:[ &-]+[a-z0-9]+)*$/;

/** Which shape a kind's key must have. Mirrors followIntentKeyRule in the API. */
function keyPattern(kind: string): RegExp {
  return kind === "tag" ? TAG_KEY_PATTERN : KEY_PATTERN;
}

/** No slug this platform issues comes near this; the cap is a bound, not a rule. */
const KEY_MAX_LENGTH = 100;

/** How an intent is spelled. The only place that decides. */
export function followIntent(kind: FollowSubjectKind, key: string): string {
  return `${kind}${SEPARATOR}${key}`;
}

/**
 * safeFollowIntent guards what may reach the sign-in flow as an intent.
 *
 * Anything that is not exactly a known kind, one separator and a well-formed key
 * comes back null — and null means "no intent", not "an error". A junk `follow`
 * on a hand-typed or tampered address must never be a reason somebody cannot
 * sign in: they get the ordinary form and land where `next` says, having simply
 * followed nothing. The API is stricter about the same string and refuses it
 * outright, which is right there: by the time a value reaches the API it has
 * passed through here, so a malformed one is a bug rather than a visitor.
 */
export function safeFollowIntent(raw: string | null | undefined): string | null {
  const value = raw?.trim() ?? "";
  if (value === "") {
    return null;
  }
  const separator = value.indexOf(SEPARATOR);
  if (separator < 0) {
    return null;
  }
  const kind = value.slice(0, separator);
  const key = value.slice(separator + 1);
  if (!(FOLLOW_SUBJECT_KINDS as readonly string[]).includes(kind)) {
    return null;
  }
  // A second separator is a second field somebody is appending to a format that
  // has none, and the honest reading of the whole string is that it is not an
  // intent.
  if (key.includes(SEPARATOR)) {
    return null;
  }
  if (key.length === 0 || key.length > KEY_MAX_LENGTH || !keyPattern(kind).test(key)) {
    return null;
  }
  return `${kind}${SEPARATOR}${key}`;
}

/**
 * followSignInHref is where the Follow control sends an anonymous visitor: the
 * existing sign-in page, carrying both halves of what they were doing.
 *
 * `next` is where they came from, so proving who they are does not cost them
 * their place — the same parameter the header's sign-in link writes, guarded by
 * the same `safeNext`, so an intent introduces no second redirect and no new way
 * to leave this Storefront. `follow` is what they pressed.
 *
 * The path is locale-free on the way through, exactly as `signInHref` is: it
 * comes from next-intl's `usePathname` with the prefix already off and gets one
 * back from `Link`, so a visitor who switches language mid sign-in still lands
 * where they were going with what they asked for intact.
 */
export function followSignInHref(
  intent: string,
  pathname: string,
  search?: string | null,
): string {
  const params = new URLSearchParams();
  const next = safeNext(pathWithQuery(pathname, search));
  params.set("next", next);
  const safe = safeFollowIntent(intent);
  if (safe) {
    params.set("follow", safe);
  }
  return `${SIGN_IN_PATH}?${params.toString()}`;
}
