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

/**
 * The one language the Terms are never published without (#559, spec #556).
 *
 * The backend calls it `terms.PrevailingLocale`: the Spanish text IS the
 * contract (§37) and the English one is a translation of it, so a publish that
 * would drop Spanish is refused outright. Here it is the language `x-default`
 * names on this path — x-default is a claim about where a reader with no
 * language preference lands, and it must never name a language an edition may
 * stop publishing — and the language the footer links across to when the
 * reader's own is not published.
 *
 * A constant and not a read, exactly as it is a constant and not a column on
 * the backend: the guarantee is that changing it costs a code change. And
 * deliberately a SECOND constant beside the Policy's, because the two duties
 * are different ones and a shared name would flatten the stronger into the
 * weaker.
 */
export const TERMS_PROTECTED_LOCALE = "es";
