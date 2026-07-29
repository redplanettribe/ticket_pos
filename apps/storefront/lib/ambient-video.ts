/**
 * Whether this visitor is sent the Event's Cover Video at all.
 *
 * The hero video is decoration: an ambient, muted, looping clip that a visitor
 * stops seeing the moment they scroll to the ticket list (ADR 0020). Two kinds
 * of visitor have asked, in the only way a browser lets them ask, not to be sent
 * decoration — and for both the answer is not a smaller video or a paused one,
 * it is no video: no `<video>` element, no `src`, no fetch.
 *
 * - **Reduced motion.** `prefers-reduced-motion: reduce` is an accessibility
 *   setting, and a full-bleed autoplaying loop behind an event's title is
 *   exactly the motion it is set to stop. The still Cover Image is the whole
 *   experience for these visitors, unchanged from before the video existed.
 * - **Data saver.** `navigator.connection.saveData` says the connection is
 *   metered or the visitor has asked the browser to spend less. This platform
 *   serves the MP4 verbatim from a CDN-less public bucket, so a hero view costs
 *   the visitor the whole file — up to the 50 MB cap — for something they cannot
 *   even hear. Not fetching it is the only honest response.
 *
 * Both signals are readable only in the browser, which is what makes this
 * decision a client one: the server renders the Cover Image and nothing else,
 * and the video element appears after mount, or never.
 *
 * This function is the rule; reading the browser for it is
 * {@link ambientVideoSignals}. Keeping them apart is what makes the rule
 * testable without a DOM, and it is a rule worth testing — "we accidentally
 * shipped the video to reduced-motion visitors" is not a bug anyone reports.
 */

/** What the browser says about this visitor's appetite for decoration. */
export type AmbientVideoSignals = {
  /** The visitor's `prefers-reduced-motion: reduce` preference. */
  prefersReducedMotion: boolean;
  /** The Save-Data / data-saver signal on the connection, when the browser has one. */
  saveData: boolean;
};

/**
 * shouldPlayAmbientVideo answers whether a hero with this video URL may load it.
 *
 * A missing URL is a "no" like any other, so the caller never has to ask two
 * questions: an Event with no Cover Video and a visitor who declined one reach
 * the identical still-image hero.
 */
export function shouldPlayAmbientVideo(
  videoUrl: string | null | undefined,
  signals: AmbientVideoSignals,
): boolean {
  if (!videoUrl) return false;
  return !signals.prefersReducedMotion && !signals.saveData;
}

/**
 * ambientVideoSignals reads the two preferences out of the browser.
 *
 * Every lookup is defensive, because both APIs are optional in practice:
 * `matchMedia` is missing under SSR and in some embedded webviews, and
 * `navigator.connection` is unimplemented outside Chromium. An absent signal is
 * read as "not asked for", which sends the video — the same thing every browser
 * did before either API existed.
 */
export function ambientVideoSignals(): AmbientVideoSignals {
  const prefersReducedMotion =
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const connection =
    typeof navigator !== "undefined"
      ? (navigator as Navigator & { connection?: { saveData?: boolean } }).connection
      : undefined;

  return {
    prefersReducedMotion: prefersReducedMotion === true,
    saveData: connection?.saveData === true,
  };
}
