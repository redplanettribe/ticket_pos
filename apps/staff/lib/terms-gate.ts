/**
 * The staff Terms gate's decision, on its own (#570, ADR 0067).
 *
 * ADR 0066 had the gate bind at the next sign-in. It never does: the Staff
 * Session's expiry slides on every authenticated request, so a daily user signs
 * in once and never again. The gate therefore binds where the person actually
 * is — on their next PAGE NAVIGATION — and the middleware asks this function,
 * per request, what to do about it.
 *
 * It is a pure function of a pathname and one flag for one reason: this is the
 * whole of the gate's logic on the client side, and the alternative way to test
 * it is a Playwright run. That is not available here — the e2e suite cannot be
 * green in one run (passcodes are rationed to 10 per IP per 15 minutes, and the
 * suite needs more sign-ins than that) and it drives a stale dev stack that
 * builds nothing. So the decision lives here, where a test can state a case in
 * one line, and middleware.ts keeps only the plumbing.
 *
 * NOTHING HERE REVOKES, RE-MINTS OR RE-PROVES ANYTHING. A diversion is a
 * redirect and no more: the session cookie is untouched, no passcode is spent,
 * and the person returns to where they were going the moment they accept.
 */

/** Where the interstitial lives. */
export const TERMS_GATE_PATH = "/terms";

/**
 * The query parameter carrying where the person was going when they were
 * stopped, so accepting returns them there rather than to the dashboard.
 */
export const RETURN_PARAM = "next";

export type TermsGateInput = {
  /** The pathname being navigated to. */
  pathname: string;
  /** Its query string including the leading "?", or "" when there is none. */
  search?: string;
  /**
   * `terms_outstanding` from the session route.
   *
   * NULL/UNDEFINED IS NOT FALSE. The backend leaves it null on every response
   * that did not ask, and on the one that asked and whose read failed — and a
   * failed consent read must not sign 25 people out or divert every navigation
   * into an interstitial that cannot be rendered. Only an explicit `true`
   * diverts.
   */
  termsOutstanding: boolean | null | undefined;
};

export type TermsGateDecision =
  | { kind: "allow" }
  /**
   * Divert this navigation. The pieces rather than a URL, because the caller
   * holds a NextURL to clone and this module must stay framework-free so a test
   * can run it under plain node.
   */
  | { kind: "interstitial"; pathname: string; search: string };

const ALLOW: TermsGateDecision = { kind: "allow" };

/**
 * What to do with one navigation.
 *
 * The order of the checks is the ruling:
 *
 * 1. `/api/` IS NEVER GATED. The gate is a navigation gate and nothing else, so
 *    a sale in progress commits and no in-flight mutation is ever refused
 *    because a legal edition rolled over at midnight. The middleware
 *    short-circuits these before it even resolves a session; the check is
 *    repeated here so the rule is testable as a rule rather than as a line's
 *    position in a file.
 * 2. The interstitial and the sign-in page pass, or the diversion is a loop.
 * 3. Everything else is diverted when — and only when — an acceptance is
 *    explicitly outstanding. `/operator` included, deliberately: the operator
 *    who published the edition meets their own interstitial, so the person who
 *    re-gated everybody has demonstrably read the text.
 */
export function decideTermsGate(input: TermsGateInput): TermsGateDecision {
  const { pathname } = input;

  if (pathname.startsWith("/api/")) {
    return ALLOW;
  }
  if (isPathWithin(pathname, TERMS_GATE_PATH) || isPathWithin(pathname, "/login")) {
    return ALLOW;
  }
  if (input.termsOutstanding !== true) {
    return ALLOW;
  }

  const target = `${pathname}${input.search ?? ""}`;
  return {
    kind: "interstitial",
    pathname: TERMS_GATE_PATH,
    search: `?${RETURN_PARAM}=${encodeURIComponent(target)}`,
  };
}

/**
 * Where an accepted interstitial sends the person: what they asked for, or the
 * app root.
 *
 * The parameter is attacker-controllable — it is in a URL somebody can be
 * handed — so it is treated as a claim rather than a destination. Only a
 * same-site absolute path survives: anything protocol-relative ("//evil.test")
 * or scheme-bearing would be an open redirect out of the staff app, and a
 * return INTO the gate would be a loop. `/api/` is refused too: those addresses
 * are for fetches, and landing a browser on one is never what the person was
 * doing.
 */
export function termsGateReturnPath(next: string | null | undefined): string {
  if (!next || !next.startsWith("/")) {
    return "/";
  }
  // "//host" and "/\host" are both read as protocol-relative by browsers.
  if (next.startsWith("//") || next.startsWith("/\\")) {
    return "/";
  }
  // A raw control character or space in a Location header is a header-splitting
  // attempt, not a path. Percent-encoded ones are ordinary and stay.
  if (/[\u0000-\u0020\u007f]/.test(next)) {
    return "/";
  }
  const pathname = next.split(/[?#]/, 1)[0];
  if (isPathWithin(pathname, TERMS_GATE_PATH) || pathname.startsWith("/api/")) {
    return "/";
  }
  return next;
}

/**
 * Whether the edition being shown asks the Adulthood Declaration (#587,
 * ADR 0069).
 *
 * THE LABEL'S PRESENCE IS THE WHOLE ANSWER, and that is the decision this
 * function exists to state once for both staff surfaces. The API omits
 * `adulthood_declaration_label` from the payload when the edition in effect
 * carries no `label-adulthood-declaration` artifact, and refuses to serve the
 * gate at all when the edition asks but cannot word the box — so "there are
 * words" and "there is a box" are the same fact, and neither surface may decide
 * it any other way. Drawing a mandatory contractual checkbox with nothing
 * written beside it is the one thing §3 forbids outright.
 *
 * Which means introducing the declaration is a PUBLISH and not a deploy: this
 * binary asks whenever the operator's edition asks, and never otherwise.
 */
export function asksAdulthoodDeclaration(
  adulthoodDeclarationLabel: string | null | undefined,
): boolean {
  return typeof adulthoodDeclarationLabel === "string" && adulthoodDeclarationLabel.trim() !== "";
}

/** The answers a staff terms surface holds when its submit button is drawn. */
export type TermsGateAnswers = {
  /** The label the API served for the second box, absent where it does not ask. */
  adulthoodDeclarationLabel?: string | null;
  /** The Terms box, always owed on these two surfaces. */
  termsAccepted: boolean;
  /** The Adulthood Declaration box, meaningful only where it was drawn. */
  adulthoodDeclared: boolean;
};

/**
 * Whether every owed required box is ticked — the enabled/disabled state of
 * both staff submit buttons, decided in one place (#587).
 *
 * THE API IS THE GUARANTEE AND THIS IS THE COURTESY. An unticked box is refused
 * by the backend with TERMS_ACCEPTANCE_REQUIRED or
 * ADULTHOOD_DECLARATION_REQUIRED whatever this returns, before anything is
 * written; a curl walks straight past a disabled button and gets the same
 * refusal. What this buys is that a person does not submit a form they cannot
 * possibly have finished.
 *
 * A box that was never drawn is never owed, so an edition that does not ask is
 * satisfied by the Terms box alone. That asymmetry is why the label is a
 * parameter here: the answer state alone cannot tell "unticked" from "never
 * shown", and treating the second as the first would disable the button on
 * every edition published before the Artifact existed.
 */
export function termsGateAnswersComplete(answers: TermsGateAnswers): boolean {
  if (!answers.termsAccepted) {
    return false;
  }
  if (asksAdulthoodDeclaration(answers.adulthoodDeclarationLabel) && !answers.adulthoodDeclared) {
    return false;
  }
  return true;
}

/** A path is within a prefix when it IS it, or is nested under it. */
function isPathWithin(pathname: string, prefix: string): boolean {
  return pathname === prefix || pathname.startsWith(`${prefix}/`);
}
