// PROTOTYPE - throwaway. Fixtures for issue #715 (SRI annulment map, issue #710).
// The local database holds no invoices, so the variants render against these.
// Every rule here restates a ruling on the map, cited inline, and nothing else.

export type Kind = "sale_invoice" | "manual";
export type Status = "authorized" | "pending" | "needs_attention" | "annulled";
export type SriCheck = "anulado" | "still_authorized" | "unknown" | null;

export type Fixture = {
  id: string;
  kind: Kind;
  status: Status;
  environment: "production" | "test";
  number: string;
  issuedOn: string; // YYYY-MM-DD, Guayaquil calendar day (fechaEmision)
  authorizedAt: string; // what the portal form asks as an identifying field
  accessKey: string;
  // Exactly as in the signed XML's identificacionComprador, spaces included.
  recipientTaxIdXml: string;
  recipientName: string;
  recipientEmail: string;
  saleRef: string | null;
  saleStanding: boolean;
  totalLabel: string;
  // Refusals from the gates ruling (#713 section 3).
  liveCreditNote: string | null;
  liveReissue: boolean;
  // After Mark annulled.
  annulledBy: string | null;
  annulledAt: string | null;
  annulNote: string | null;
  sriCheck: SriCheck;
  sriCheckedAt: string | null;
  lateAcknowledged: boolean;
};

export type Scenario = {
  key: string;
  name: string;
  // The prototype's "today" in Guayaquil, so the window can be shown at every stage.
  today: string;
  invoice: Fixture;
  // What the best-effort ConsultaComprobante answers after recording (#713 section 5).
  checkAnswers: SriCheck;
};

const base: Fixture = {
  id: "proto-1",
  kind: "sale_invoice",
  status: "authorized",
  environment: "production",
  number: "001-001-000000123",
  issuedOn: "2026-11-10",
  authorizedAt: "2026-11-10T14:22:05-05:00",
  accessKey: "1011202601179200000100120010010000001231234567814",
  recipientTaxIdXml: "1712345678 001",
  recipientName: "Comercial Andina S.A.",
  recipientEmail: "facturas@comercialandina.ec",
  saleRef: "TP-7K2Q-9MXA",
  saleStanding: true,
  totalLabel: "$ 46,00",
  liveCreditNote: null,
  liveReissue: false,
  annulledBy: null,
  annulledAt: null,
  annulNote: null,
  sriCheck: null,
  sriCheckedAt: null,
  lateAcknowledged: false,
};

export const SCENARIOS: Scenario[] = [
  {
    key: "open",
    name: "Authorized, window open (22 days left)",
    today: "2026-11-15",
    invoice: base,
    checkAnswers: "anulado",
  },
  {
    key: "last-days",
    name: "Authorized, last 2 days of the window",
    today: "2026-12-06",
    invoice: { ...base, issuedOn: "2026-11-28", authorizedAt: "2026-11-28T09:02:41-05:00" },
    checkAnswers: "anulado",
  },
  {
    key: "closed",
    name: "Authorized, past day 7 (record only, typed acknowledgement)",
    today: "2026-12-09",
    invoice: { ...base, issuedOn: "2026-11-03", authorizedAt: "2026-11-03T18:40:12-05:00" },
    checkAnswers: "anulado",
  },
  {
    key: "credited",
    name: "Authorized, a live Credit Note refuses annulment",
    today: "2026-11-15",
    invoice: { ...base, liveCreditNote: "001-002-000000041", saleStanding: false },
    checkAnswers: "anulado",
  },
  {
    key: "pending",
    name: "Pending at the SRI (same act, same gates)",
    today: "2026-11-15",
    invoice: { ...base, status: "pending" },
    checkAnswers: "anulado",
  },
  {
    key: "mismatch",
    name: "Annulled, but the SRI still reports AUTORIZADO",
    today: "2026-11-15",
    invoice: { ...base, status: "authorized" },
    checkAnswers: "still_authorized",
  },
  {
    key: "unknown",
    name: "Annulled, the SRI check failed in transport",
    today: "2026-11-15",
    invoice: { ...base, status: "authorized" },
    checkAnswers: "unknown",
  },
  {
    key: "reversed",
    name: "Reversed Sale, its Credit Note died (the row-39 cure)",
    today: "2026-11-15",
    invoice: { ...base, saleStanding: false },
    checkAnswers: "anulado",
  },
  {
    key: "manual",
    name: "Manual Tax Invoice, authorized",
    today: "2026-11-15",
    invoice: { ...base, kind: "manual", saleRef: null, number: "001-001-000000124" },
    checkAnswers: "anulado",
  },
  {
    key: "test",
    name: "Test environment (celcer), record only in effect",
    today: "2026-11-15",
    invoice: { ...base, environment: "test" },
    checkAnswers: "anulado",
  },
];

// Day 7 of the month after the month of issued_on, in Guayaquil, strictly,
// with no holiday table (#713 section 2).
export function deadlineFor(issuedOn: string): string {
  const [y, m] = issuedOn.split("-").map(Number);
  const nextYear = m === 12 ? y + 1 : y;
  const nextMonth = m === 12 ? 1 : m + 1;
  return `${nextYear}-${String(nextMonth).padStart(2, "0")}-07`;
}

function dayNumber(day: string): number {
  const [y, m, d] = day.split("-").map(Number);
  return Date.UTC(y, m - 1, d) / 86_400_000;
}

export type Window = {
  deadline: string;
  daysLeft: number; // 0 means today is the deadline
  state: "open" | "last_days" | "closed";
  // For the timeline: how far through the window today is, 0..1.
  progress: number;
};

export function windowFor(issuedOn: string, today: string): Window {
  const deadline = deadlineFor(issuedOn);
  const daysLeft = dayNumber(deadline) - dayNumber(today);
  const span = dayNumber(deadline) - dayNumber(issuedOn);
  const progress = Math.min(1, Math.max(0, (dayNumber(today) - dayNumber(issuedOn)) / span));
  const state = daysLeft < 0 ? "closed" : daysLeft <= 1 ? "last_days" : "open";
  return { deadline, daysLeft, state, progress };
}

export function formatDay(day: string): string {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });
}

export function formatMoment(iso: string): string {
  return new Date(iso).toLocaleString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "America/Guayaquil",
  });
}

// Why Mark annulled is not offered, or null when it is (#713 section 3).
export function refusal(invoice: Fixture): string | null {
  if (invoice.status === "annulled") return null;
  if (invoice.liveCreditNote) {
    return `A Credit Note (${invoice.liveCreditNote}) stands against this factura, so it cannot also be annulled. Annulling would leave an authorized Credit Note pointing at a dead factura.`;
  }
  if (invoice.liveReissue) {
    return "A Reissue of this factura is in flight, so it cannot be annulled.";
  }
  return null;
}

// What the act would leave behind, as the follow-ups the one-act ruling allows (#712).
export type FollowUp = {
  key: "issue_again" | "reissue" | "reversal" | "retype" | "nothing";
  title: string;
  owes: string;
  buyerHears: string;
  action: string | null;
};

export function followUps(invoice: Fixture): FollowUp[] {
  if (invoice.kind === "manual") {
    return [
      {
        key: "retype",
        title: "Type it again by hand",
        owes: "Nothing automatic. A new manual Tax Invoice, if one is wanted, is created from the list as today.",
        buyerHears: "Nothing from the platform. Manual Tax Invoices send no mail (#714).",
        action: "New Tax Invoice",
      },
    ];
  }
  if (!invoice.saleStanding) {
    return [
      {
        key: "nothing",
        title: "Nothing follows",
        owes: "The Sale is already reversed and now carries no live factura. Nothing is owed.",
        buyerHears: "The Annulment notice for a reversed purchase: nothing else follows (#714).",
        action: null,
      },
    ];
  }
  return [
    {
      key: "issue_again",
      title: "Issue again",
      owes: "A fresh factura, same lines, amounts and Recipient, under a new number.",
      buyerHears: "The fresh factura's delivery mail, saying it replaces one annulled at the SRI.",
      action: "Issue again",
    },
    {
      key: "reissue",
      title: "Reissue to a corrected Recipient",
      owes: "A corrected factura and no Credit Note, since nothing stands at the SRI to cancel.",
      buyerHears: "The corrected factura's delivery mail, saying it replaces one annulled at the SRI.",
      action: "Reissue",
    },
    {
      key: "reversal",
      title: "Reverse the Sale (Operator Reversal)",
      owes: "No Credit Note: the reversal finds the factura dead. Tickets are voided.",
      buyerHears: "The Sale Voided mail, unchanged. The Annulment notice already covered the factura.",
      action: "Open the Sale",
    },
  ];
}
