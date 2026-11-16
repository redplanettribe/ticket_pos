/**
 * The annulment panel on a factura's detail page (#745, spec #742, ADR 0078),
 * decided once and tested under `node --test`.
 *
 * The API is the gate and the clock: `annulability` says whether Mark annulled
 * is admitted, and if not why, and carries the Annulment Window with every date
 * already worked out in Guayaquil. What is left for the page is which face of
 * the panel to draw, where today falls on the window's timeline, which tone the
 * "Annullable until" line takes, and when the record form may be pressed. It
 * never computes a date.
 */

import type {
  AnnulmentVerdict,
  EcuadorIssuerEnvironment,
  InvoiceKind,
  InvoiceStatus,
  OperatorAnnulmentCheck,
  OperatorAnnulmentFollowUp,
  OperatorAnnulmentNotice,
  OperatorAnnulmentPortal,
  OperatorAnnulmentWindow,
  OperatorInvoiceAnnulability,
} from "./operator-api";

/**
 * The refusals a factura's panel shrinks to one sentence for (story 29): facts
 * about the document that keep Mark annulled from it while it otherwise could
 * be annulled. Any other refusal - owed, unsigned, withdrawn, abandoned - is a
 * state the act never reaches, and the panel is not drawn at all.
 */
export const ANNULMENT_PANEL_REFUSALS = [
  "INVOICE_ABANDON_INSTEAD",
  "INVOICE_CREDIT_NOTE_STANDS",
  "INVOICE_REISSUE_STANDS",
] as const;

export type AnnulmentPanelMode = "record" | "annulled" | "refused" | null;

/**
 * Which face the panel shows: the record form on a factura the act admits,
 * "Annulled at the SRI" once it was annulled, one sentence where a fact about
 * it refuses, and nothing elsewhere. A Credit Note has no annulability and no
 * panel: it keeps the SRI-remedies card's Mark annulled.
 */
export function annulmentPanelMode(invoice: {
  status: InvoiceStatus;
  annulability: OperatorInvoiceAnnulability | null;
}): AnnulmentPanelMode {
  const annulability = invoice.annulability;
  if (annulability === null) {
    return null;
  }
  if (invoice.status === "annulled") {
    return "annulled";
  }
  if (annulability.admitted) {
    return "record";
  }
  const code = annulability.refusal?.code;
  if (code !== undefined && (ANNULMENT_PANEL_REFUSALS as readonly string[]).includes(code)) {
    return "refused";
  }
  return null;
}

/**
 * Where today falls on the timeline from the Emission Date to the deadline, as
 * a fraction from 0 to 1: at the end on the last day and once closed.
 */
export function annulmentWindowProgress(window: OperatorAnnulmentWindow): number {
  if (!window.open || window.days_total <= 0) {
    return 1;
  }
  const elapsed = window.days_total - window.days_left;
  return Math.min(1, Math.max(0, elapsed / window.days_total));
}

/**
 * The "Annullable until" line's tone: quiet while open, a warning from day 1 of
 * the following month to the deadline (story 16), greyed once closed.
 */
export function annulmentWindowTone(window: OperatorAnnulmentWindow): "open" | "warning" | "closed" {
  if (!window.open) {
    return "closed";
  }
  return window.warning ? "warning" : "open";
}

/**
 * Whether the record form may be pressed: the number typed in full, and past
 * the window the acknowledgement typed as the phrase the form names, in the
 * reader's language and whatever its case. The API checks the number again
 * and refuses a mismatch; this only keeps a certain refusal from being sent.
 */
export function annulmentRecordReady(input: {
  number: string;
  typedNumber: string;
  late: boolean;
  typedAcknowledgement: string;
  phrase: string;
}): boolean {
  if (input.typedNumber.trim() !== input.number) {
    return false;
  }
  if (!input.late) {
    return true;
  }
  return input.typedAcknowledgement.trim().toLocaleLowerCase() === input.phrase.trim().toLocaleLowerCase();
}

/**
 * The Annulment Check's one state line on the annulled face (#746), decided
 * from the API's `annulment.check` alone:
 *
 * - checking since the recording, with the next ask;
 * - ANULADO at the ask that read it;
 * - still reported as authorized since the ask that settled the mismatch;
 * - the SRI didn't confirm at the +60 min ask, asking every 6 h until the
 *   schedule's end, or `until` null once the asks have ended;
 * - unchecked: recorded before the check existed, never asked.
 */
export type AnnulmentCheckLine =
  | { kind: "checking"; since: string; next: string | null }
  | { kind: "confirmed"; at: string }
  | { kind: "mismatch"; since: string }
  | { kind: "unconfirmed"; at: string; until: string | null }
  | { kind: "unchecked" };

export function annulmentCheckLine(check: OperatorAnnulmentCheck, annulledAt: string): AnnulmentCheckLine {
  if (!check.scheduled) {
    return { kind: "unchecked" };
  }
  switch (check.verdict) {
    case "confirmed":
      return { kind: "confirmed", at: check.since ?? annulledAt };
    case "mismatch":
      return { kind: "mismatch", since: check.since ?? annulledAt };
    case "unconfirmed":
      return {
        kind: "unconfirmed",
        at: check.since ?? annulledAt,
        until: check.next_ask_at === null ? null : check.asks_until,
      };
    default:
      return { kind: "checking", since: annulledAt, next: check.next_ask_at };
  }
}

/**
 * Whether the panel offers a Check status row: whenever the check is not
 * confirmed. On an annulled factura it is one more ask of ConsultaComprobante,
 * feeding the same verdict and leaving the schedule alone.
 */
export function annulmentCheckOffersCheckStatus(check: Pick<OperatorAnnulmentCheck, "verdict">): boolean {
  return check.verdict !== "confirmed";
}

/** The check badge's tone, one per verdict. */
export const ANNULMENT_CHECK_BADGE: Record<AnnulmentVerdict, "default" | "secondary" | "destructive" | "outline"> = {
  checking: "secondary",
  confirmed: "default",
  mismatch: "destructive",
  unconfirmed: "outline",
};

// The portal runbook on the record face (#752, stories 22 to 28). The SRI
// offers no web service for annulment, so the panel walks the operator
// through SRI en linea: sign in, open the annulment form by the menu path of
// the factura's environment, fill it in with every field copied from the API's
// `annulability.portal`, and press Aceptar. The ticks are the page's own and
// gate nothing: the record form beside them never reads them.

/** Where the checklist's first step signs in. */
export const SRI_EN_LINEA_URL = "https://srienlinea.sri.gob.ec/sri-en-linea/inicio/NAT";

export const ANNULMENT_PORTAL_STEPS = ["sign_in", "open_form", "fill", "accept"] as const;

export type AnnulmentPortalStep = (typeof ANNULMENT_PORTAL_STEPS)[number];

/** The menu path's copy, Produccion or Pruebas by the factura's environment. */
export function annulmentPortalPathKey(
  environment: EcuadorIssuerEnvironment,
): "invoicingAnnulPortalPathProduction" | "invoicingAnnulPortalPathTest" {
  return environment === "test" ? "invoicingAnnulPortalPathTest" : "invoicingAnnulPortalPathProduction";
}

export type AnnulmentPortalField = {
  field: "access_key" | "authorization_day" | "receptor_id" | "receptor_email";
  /** Exactly what the copy button puts on the clipboard, as the API sent it. */
  value: string;
  /** Whether the value is drawn with its whitespace visible. */
  showWhitespace: boolean;
};

/**
 * The fields the portal's form asks for, in its order, each copied exactly as
 * the API sent it: the clave, the authorization's day on an authorized
 * document, the receptor ID as the signed XML carries it with its spaces
 * drawn, and the receptor email when the Recipient has one.
 */
export function annulmentPortalFields(portal: OperatorAnnulmentPortal): AnnulmentPortalField[] {
  const fields: AnnulmentPortalField[] = [{ field: "access_key", value: portal.access_key, showWhitespace: false }];
  if (portal.authorization_day !== null) {
    fields.push({ field: "authorization_day", value: portal.authorization_day, showWhitespace: false });
  }
  fields.push({ field: "receptor_id", value: portal.receptor_id, showWhitespace: true });
  if (portal.receptor_email !== "") {
    fields.push({ field: "receptor_email", value: portal.receptor_email, showWhitespace: false });
  }
  return fields;
}

/**
 * A value cut into runs, every whitespace character its own segment, so the
 * page can draw each one visibly and the operator types what the SRI holds.
 * The segments join back to the value exactly.
 */
export function visibleWhitespace(value: string): { text: string; space: boolean }[] {
  return value.split(/(\s)/).filter((text) => text !== "").map((text) => ({ text, space: /^\s$/.test(text) }));
}

/** Whether recording sends the buyer an Annulment Notice: on a Sale Invoice only, never a manual Tax Invoice. */
export function annulmentSendsNotice(kind: InvoiceKind): boolean {
  return kind === "sale";
}

/** The trail's buyer-notice line per state (#748, story 76). */
export const ANNULMENT_NOTICE_LINE_KEYS = {
  held: "invoicingAnnulPanelNoticeHeld",
  due: "invoicingAnnulPanelNoticeDue",
  sent: "invoicingAnnulPanelNoticeSent",
  dropped: "invoicingAnnulPanelNoticeDropped",
  not_owed: "invoicingAnnulPanelNoticeNotOwed",
} as const satisfies Record<OperatorAnnulmentNotice["state"], string>;

/**
 * The trail's buyer-notice line (#748, story 76): "waiting for the SRI to
 * report ANULADO" while held, "sent <time>" once sent, or that no notice is
 * owed. `sentAt` is the API's instant, for the page to format; null unless
 * sent.
 */
export function annulmentNoticeLine(notice: OperatorAnnulmentNotice): {
  key: (typeof ANNULMENT_NOTICE_LINE_KEYS)[OperatorAnnulmentNotice["state"]];
  sentAt: string | null;
} {
  return {
    key: ANNULMENT_NOTICE_LINE_KEYS[notice.state],
    sentAt: notice.state === "sent" ? notice.sent_at : null,
  };
}

// The follow-up chooser on the annulled face (#747, stories 54 to 58, 63 and
// 64). The API decides which follow-ups an annulled factura has, by the very
// gates their presses answer with (`annulment.follow_ups`): Issue again,
// Reissue and Reverse the Sale on a standing Sale's factura, "nothing follows"
// on a reversed Sale's, "type it again by hand" on a manual Tax Invoice, and
// Issue again and Reissue refused with INVOICE_ANNULMENT_MISMATCH while the
// SRI still reports the factura authorized. This only sorts them into cards
// and held refusals, and says the two things the operator must read plainly.

export type FollowUpKind = OperatorAnnulmentFollowUp["kind"];

/** Each card's copy: its title, what it owes, and what the buyer hears. */
export const FOLLOW_UP_CARD_KEYS = {
  issue_again: {
    title: "invoicingFollowUpIssueAgainTitle",
    owes: "invoicingFollowUpIssueAgainOwes",
    buyer: "invoicingFollowUpIssueAgainBuyer",
  },
  reissue: {
    title: "invoicingFollowUpReissueTitle",
    owes: "invoicingFollowUpReissueOwes",
    buyer: "invoicingFollowUpReissueBuyer",
  },
  reverse_sale: {
    title: "invoicingFollowUpReverseTitle",
    owes: "invoicingFollowUpReverseOwes",
    buyer: "invoicingFollowUpReverseBuyer",
  },
  nothing_follows: {
    title: "invoicingFollowUpNothingTitle",
    owes: "invoicingFollowUpNothingOwes",
    buyer: "invoicingFollowUpNothingBuyer",
  },
  type_again: {
    title: "invoicingFollowUpTypeAgainTitle",
    owes: "invoicingFollowUpTypeAgainOwes",
    buyer: "invoicingFollowUpTypeAgainBuyer",
  },
} as const satisfies Record<FollowUpKind, { title: string; owes: string; buyer: string }>;

export type FollowUpChooser = {
  /** The admitted follow-ups, one card each, in the API's order. */
  cards: FollowUpKind[];
  /** The refused ones, each with the code and message its press would answer. */
  held: { kind: FollowUpKind; code: string; message: string }[];
  /**
   * The Sale stands and no factura stands for it: "This Sale now has no
   * factura", and leaving it keeps the Sale in the needs-attention queue.
   */
  saleHasNoFactura: boolean;
  /**
   * The SRI has not confirmed the annulment: the check is still checking, or
   * reads unconfirmed. Issue again's and Reissue's confirms say so. (At
   * mismatch both are held, and once confirmed nothing is left to say.)
   */
  notConfirmed: boolean;
};

/**
 * The chooser of an annulled factura, or null where there is none: not
 * annulled, or a factura already stands for its Sale (the chain card names
 * it).
 */
export function followUpChooser(annulment: {
  follow_ups: readonly OperatorAnnulmentFollowUp[] | null;
  check: Pick<OperatorAnnulmentCheck, "verdict"> | null;
}): FollowUpChooser | null {
  const followUps = annulment.follow_ups;
  if (followUps === null || followUps.length === 0) {
    return null;
  }
  const cards: FollowUpKind[] = [];
  const held: FollowUpChooser["held"] = [];
  for (const f of followUps) {
    if (f.admitted) {
      cards.push(f.kind);
    } else if (f.refusal !== null) {
      held.push({ kind: f.kind, code: f.refusal.code, message: f.refusal.message });
    }
  }
  return {
    cards,
    held,
    // The API lists Issue again, admitted or held, exactly where the Sale
    // stands and no factura stands for it (service.annulmentFollowUps).
    saleHasNoFactura: followUps.some((f) => f.kind === "issue_again"),
    notConfirmed: annulment.check?.verdict === "checking" || annulment.check?.verdict === "unconfirmed",
  };
}
