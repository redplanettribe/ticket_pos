/**
 * Where the Términos y Condiciones page lives, with no language in it (#535).
 *
 * One constant for the same three sites the Privacy Policy's constant serves —
 * the page itself, the footer link on every page's chrome, and the sitemap that
 * declares both languages of it to a crawler — and for its reason: those three
 * must never disagree about where the contract is published. The Staff app's
 * sign-in gate (#538) links here too rather than hosting a copy of the
 * document.
 *
 * Framework-free, like lib/privacy-policy.ts, so the sitemap's unit tests can
 * import it without pulling in a Next page.
 */
export const TERMS_PATH = "/terms";
