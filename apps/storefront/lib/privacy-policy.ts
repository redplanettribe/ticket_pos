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
