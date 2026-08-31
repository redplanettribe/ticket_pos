// The consent access log, client side (#569, spec #556, ADR 0067): the
// platform's record of its own reads of people's data.
//
// A framework-free module so the three rules that are easy to get wrong — the
// closed act vocabulary, how a filter becomes a query string, and the fact that
// THERE IS NO SUBJECT FILTER — can be tested with no browser and no server (see
// access-log.test.ts). The fetcher lives next door for the reason the
// acceptance browsers' does.
//
// WHAT THIS SCREEN IS. Every other surface in the Legal Center can be defended
// by pointing at a row: a publication has its provenance, a withdrawal has its
// Consent Record, a handover has its pack row. A READ HAS NOTHING — somebody
// looked, and nothing about this platform's ordinary workings records that — so
// four acts write a row of their own, and this is where they are read.

/**
 * The four acts, and only four. They match migration 116's CHECK, so the
 * vocabulary is closed in the same shape on both sides of the wire.
 *
 * `audit_read` IS IN THE LIST because reading this log is itself a touch of
 * people's data. A surface that did not audit its own reader would have a hole
 * in it shaped exactly like the person most likely to use it.
 */
export type AccessAct = "list_read" | "subject_read" | "evidence_export" | "audit_read";

/** The acts the filter offers, in the order the screen shows them. */
export const ACCESS_ACTS: readonly AccessAct[] = [
  "list_read",
  "subject_read",
  "evidence_export",
  "audit_read",
] as const;

/**
 * One logged act.
 *
 * EVERY OPTIONAL FIELD IS NULL WHERE THE ACT HAS NO SUCH THING, never zero and
 * never "". A `subject_read` has no result count, and a 0 rendered in its place
 * would be a claim the row does not make.
 */
export type AccessEntry = {
  id: number;
  act: AccessAct;
  /** The operator who looked. From their Staff Session, never from a body. */
  actor_email: string;
  occurred_at: string;

  /** The question a list read asked. Null on the two acts that name a person. */
  population: "customer" | "staff" | null;
  document: "policy" | "terms" | null;
  status_filter: string | null;
  /**
   * WHETHER a term narrowed the page, and never what the term was.
   *
   * A boolean the whole way down, so that an operator looking one person up
   * cannot thereby write that person's address into an audit log — which would
   * be this feature causing exactly the harm it exists to detect.
   */
  searched: boolean | null;
  result_count: number | null;

  /** Who was looked at. Null on the two acts that ask about a population. */
  subject_customer_id: string | null;
  /** A plain address — never a Staff Digest, which a key rotation would orphan. */
  subject_email: string | null;

  /** The fingerprint of the file handed over, on an export alone. */
  pack_sha256: string | null;
};

export type AccessLogPage = {
  entries: AccessEntry[];
  /** Null on the last page. Its presence is the whole "is there more?" signal. */
  next_cursor: string | null;
};

/**
 * What a page request carries: ACTOR, ACT AND DATE.
 *
 * THERE IS NO SUBJECT FIELD, and its absence is the design rather than an
 * omission. An audit log searchable by the person it is about would be a second
 * way to look people up — keyed on the record of people being looked up, and
 * available to precisely the role whose looking this exists to record. The type
 * has nowhere to put one so that no screen is one line away from having it.
 */
export type AccessLogRequest = {
  /** One operator's acts. An exact address; empty means everybody. */
  actor?: string;
  /** One of the four; empty means all of them. */
  act?: AccessAct | "";
  /** Calendar days, YYYY-MM-DD, in UTC. `to` includes its whole day. */
  from?: string;
  to?: string;
  cursor?: string | null;
};

/**
 * The BFF path for one page, filters and all.
 *
 * THE FILTERS TRAVEL IN A QUERY STRING, unlike the acceptance browsers' POSTed
 * body — and it is the same rule (#565) reaching a different answer. Theirs
 * carry a search fragment and a cursor that IS an email address; nothing here is
 * a data subject: the actor is a platform operator, whose address is on an
 * allowlist rather than in the population being audited, the act is a
 * vocabulary word, the dates are dates, and the cursor is a timestamp and a row
 * id. The rule is no data subject in a request line, not "no query strings".
 *
 * EMPTY FILTERS ARE OMITTED rather than sent blank, so "everything" is one thing
 * on the wire and not several.
 */
export function accessLogPath(request: AccessLogRequest = {}): string {
  const params = new URLSearchParams();
  const add = (key: string, value: string | null | undefined) => {
    const trimmed = (value ?? "").trim();
    if (trimmed !== "") {
      params.set(key, trimmed);
    }
  };
  add("actor", request.actor);
  add("act", request.act);
  add("from", request.from);
  add("to", request.to);
  add("cursor", request.cursor);
  const query = params.toString();
  return query === "" ? "/api/operator/legal/access-log" : `/api/operator/legal/access-log?${query}`;
}

/**
 * The copy key for one act, e.g. `actListRead`.
 *
 * Derived rather than mapped, so a fifth act cannot be added to the type without
 * a missing key showing up at the call site.
 */
export function accessActLabelKey(act: AccessAct): string {
  switch (act) {
    case "list_read":
      return "actListRead";
    case "subject_read":
      return "actSubjectRead";
    case "evidence_export":
      return "actEvidenceExport";
    case "audit_read":
      return "actAuditRead";
  }
}

/**
 * How one row's subject is named on screen.
 *
 * THE ADDRESS IS SHOWN AND IT IS NOT A URL. Showing who was looked at is the
 * point of a `subject_read` row — the subject IS the act — and the rule that
 * has always applied is that the address must not reach a request line, which
 * this does not: the row's link, where there is one, carries the opaque Customer
 * id the act was performed under.
 *
 * A list read returns "" rather than a placeholder, because it named nobody:
 * printing "—" in a subject column is honest, and printing "everybody" would not
 * be.
 */
export function accessSubjectLabel(entry: AccessEntry): string {
  return entry.subject_email ?? "";
}

/**
 * The link back to the record an act was performed against, or null.
 *
 * ONLY WHERE THE ACT CARRIED A CUSTOMER ID. A staff subject is recorded as a
 * plain address and never as a digest (a rotation would orphan the row), so
 * there is nothing to link to: reconstructing a digest here would mean minting
 * one client-side from an address, which is the one thing the digest exists to
 * make impossible.
 */
export function accessSubjectHref(entry: AccessEntry): string | null {
  if (!entry.subject_customer_id) {
    return null;
  }
  return `/operator/legal/acceptances/customers/${encodeURIComponent(entry.subject_customer_id)}`;
}

/**
 * The first sixteen characters of a pack's fingerprint — which is also the
 * fingerprint's half that names the file (ADR 0067:
 * `consent-evidence-<sha256[:16]>-<date>.zip`), so a file in somebody's mailbox
 * can be matched to its row by eye.
 */
export function packFingerprintLabel(sha256: string): string {
  return sha256.slice(0, 16);
}
