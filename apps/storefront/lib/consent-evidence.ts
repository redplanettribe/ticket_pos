/**
 * The headers that carry a capture act's circumstances to the API.
 *
 * The API never sees a browser (ADR 0008): every consent surface reaches it
 * through a BFF route on this origin, so the user agent and the page the act
 * happened on only exist here, in the request this process received. Relaying
 * them is what lets the Go side record a technical proof at all — and it is why
 * `consent.EvidenceFromRequest` on the other end reads headers rather than a
 * body it could not trust.
 *
 * ONE SPELLING, for the same reason its Go counterpart is one function. Five
 * routes need these headers — the sign-in consent step, the confirmation link,
 * the digest toggle, the unsubscribe link and the checkout — and five copies is
 * five chances for one surface to relay something slightly different. The value
 * of an evidence column is that a compliance officer can say what it means; two
 * readings that drift would make one column mean two things depending on which
 * page somebody happened to be on.
 *
 * The IP is delegated to `clientIpHeaders`, which is empty when the address is
 * unknown so the API falls back to the transport peer rather than to a value an
 * attacker chose. The other two default to the empty string, which the API
 * stores as NULL: absent evidence is recorded as absent, never as a guess.
 */

import { clientIpHeaders } from "./client-ip.ts";

export function consentEvidenceHeaders(headers: Headers): Record<string, string> {
  return {
    ...clientIpHeaders(headers),
    "User-Agent": headers.get("user-agent") ?? "",
    Referer: headers.get("referer") ?? "",
  };
}
