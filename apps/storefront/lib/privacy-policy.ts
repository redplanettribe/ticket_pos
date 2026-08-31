/**
 * Where the Privacy Policy page lives, with no language in it (#250).
 *
 * One constant rather than the string written at each of its three sites: the
 * page itself, the footer link on every page's chrome, and the sitemap that
 * declares both languages of it to a crawler. Those are exactly the three that
 * must never disagree — a footer pointing at an address the sitemap does not
 * publish is a legal document nobody can find, and a sitemap publishing an
 * address no page answers is a 404 offered to a crawler.
 *
 * It lives here, in a framework-free module, so the sitemap's unit tests can
 * import it under `node --experimental-strip-types` without pulling in a Next
 * page — and so the shell that draws the footer link does not have to import
 * from a route file, which would make the page and its chrome import each
 * other.
 */
export const PRIVACY_POLICY_PATH = "/privacy-policy";

/**
 * The one language the Privacy Policy is never published without (#559,
 * spec #556).
 *
 * The backend calls it `policy.MandatoryLocale` and foots it on the LOPDP's
 * duty to give this notice in Spanish; a publish that would drop it is refused
 * outright there. This is the Storefront's half of that fact: the language
 * `x-default` names on this path, and the language the footer links across to
 * when the reader's own is not published. See lib/terms.ts for why the Terms
 * carry their own constant rather than sharing this one.
 */
export const PRIVACY_POLICY_PROTECTED_LOCALE = "es";

/**
 * The address to write to about personal data protection (#269).
 *
 * IT IS THE ONE THE PUBLISHED POLICY NAMES, and it is not this app's to choose.
 * The policy gives it as the contact for the controller and the Data Protection
 * Officer, and as the address for exercising every right over one's data —
 * including the one this product deliberately does not implement, erasure. The
 * Withdraw All disclosure is the only place in the product that points at that
 * escalation route, so an address invented here would be an escalation route to
 * nowhere.
 *
 * A constant rather than a message key, for the reason the brand name is one: it
 * reads the same in every language, and copying it into each catalog is how one
 * of them eventually comes to hold a different address from the legal text.
 * Changing it means changing the policy first — the policy is the source, this
 * is the citation.
 */
export const DATA_PROTECTION_EMAIL = "info@redplanettribe.org";
