// PROTOTYPE — throwaway. Answers issue #543 ("Prototype the edition editor and
// the publish confirm step") on the Legal Center map, issue #540.
//
// Everything here is in memory. No mutation reaches the backend.

import { PUBLISHED_POLICY, PUBLISHED_TERMS } from "./fixture-content";

export type Locale = "en" | "es";
export const LOCALES: Locale[] = ["en", "es"];
export const LOCALE_NAME: Record<Locale, string> = { en: "English", es: "Español" };

export type DocumentKind = "policy" | "terms";

export type SlugSpec = {
  slug: string;
  /** The hash preimage order decided in #541. Data, not Go field order. */
  ordinal: number;
  title: string;
  /** Where the operator will see this text in the wild. */
  surface: string;
  /** A one-line label vs. a multi-page legal document — they need different affordances. */
  size: "line" | "document";
};

export const POLICY_SLUGS: SlugSpec[] = [
  { slug: "short-notice", ordinal: 1, title: "Short notice", surface: "Shown above the checkout consent boxes", size: "document" },
  { slug: "label-policy-acceptance", ordinal: 2, title: "Policy acceptance label", surface: "The required checkbox at sign-up", size: "line" },
  { slug: "label-marketing-consent", ordinal: 3, title: "Marketing consent label", surface: "Optional checkbox", size: "line" },
  { slug: "label-networking-consent", ordinal: 4, title: "Networking consent label", surface: "Optional checkbox", size: "line" },
  { slug: "policy", ordinal: 5, title: "Privacy Policy", surface: "The public /privacy-policy page", size: "document" },
];

export const TERMS_SLUGS: SlugSpec[] = [
  { slug: "label-terms-acceptance", ordinal: 1, title: "Terms acceptance label", surface: "Checkbox at sign-up and before a Staff Session", size: "line" },
  { slug: "terms", ordinal: 2, title: "Términos y Condiciones", surface: "The public /terms page", size: "document" },
];

/** slug -> locale -> body. Missing key = the artifact is not written in that language. */
export type ArtifactSet = Record<string, Partial<Record<Locale, string>>>;

function invert(byLocale: Record<string, Record<string, string>>): ArtifactSet {
  const out: ArtifactSet = {};
  for (const locale of LOCALES) {
    for (const [slug, body] of Object.entries(byLocale[locale] ?? {})) {
      out[slug] = { ...(out[slug] ?? {}), [locale]: body };
    }
  }
  return out;
}

export const PUBLISHED: Record<DocumentKind, ArtifactSet> = {
  policy: invert(PUBLISHED_POLICY as unknown as Record<string, Record<string, string>>),
  terms: invert(PUBLISHED_TERMS as unknown as Record<string, Record<string, string>>),
};

// The draft starts life as a realistic work-in-progress: one artifact edited in
// both languages, one edited in English only (the Spanish lags — the failure
// mode the completeness rule exists to catch), and one brand-new artifact whose
// Spanish has not been written at all.
export const NEW_SLUG: SlugSpec = {
  slug: "label-analytics-consent",
  ordinal: 5,
  title: "Analytics consent label",
  surface: "Optional checkbox (new in this edition)",
  size: "line",
};

export function initialDraft(): ArtifactSet {
  const draft: ArtifactSet = JSON.parse(JSON.stringify(PUBLISHED.policy));
  draft["short-notice"] = {
    en: (draft["short-notice"].en ?? "").replace("profile photo", "profile photo and analytics identifiers"),
    es: (draft["short-notice"].es ?? "").replace("foto de perfil", "foto de perfil e identificadores de analítica"),
  };
  draft["label-marketing-consent"] = {
    en: `${draft["label-marketing-consent"].en ?? ""} You can unsubscribe from any message we send.`,
    es: draft["label-marketing-consent"].es,
  };
  draft[NEW_SLUG.slug] = {
    en: "I authorize **Multiticketing** to measure how I use the platform in order to improve it. Optional, and I can withdraw it at any time.",
  };
  return draft;
}

/** The slug list for a draft: the published slugs plus anything the draft added, in ordinal order. */
export function draftSlugs(draft: ArtifactSet): SlugSpec[] {
  const specs = [...POLICY_SLUGS];
  if (draft[NEW_SLUG.slug]) {
    specs.splice(4, 0, NEW_SLUG);
  }
  return specs.map((spec, index) => ({ ...spec, ordinal: index + 1 }));
}

export type CellStatus = "unchanged" | "modified" | "added" | "removed" | "missing";

export function cellStatus(published: ArtifactSet, draft: ArtifactSet, slug: string, locale: Locale): CellStatus {
  const before = published[slug]?.[locale];
  const after = draft[slug]?.[locale];
  if (!before && !after) return "missing";
  if (!before) return after?.trim() ? "added" : "missing";
  if (!after) return "removed";
  if (!after.trim()) return "missing";
  return before.trim() === after.trim() ? "unchanged" : "modified";
}

/** Publish is refused unless every artifact exists in every published language. */
export function completeness(published: ArtifactSet, draft: ArtifactSet) {
  const gaps: { slug: string; locale: Locale }[] = [];
  for (const spec of draftSlugs(draft)) {
    for (const locale of LOCALES) {
      if (cellStatus(published, draft, spec.slug, locale) === "missing") {
        gaps.push({ slug: spec.slug, locale });
      }
    }
  }
  return { complete: gaps.length === 0, gaps };
}

/** True when the draft changes the artifact set itself, not just its wording. */
export function isStructuralChange(published: ArtifactSet, draft: ArtifactSet) {
  return draftSlugs(draft).some((spec) =>
    LOCALES.some((locale) => {
      const status = cellStatus(published, draft, spec.slug, locale);
      return status === "added" || status === "removed";
    }),
  );
}

export function changedCells(published: ArtifactSet, draft: ArtifactSet) {
  const cells: { slug: string; locale: Locale; status: CellStatus }[] = [];
  for (const spec of draftSlugs(draft)) {
    for (const locale of LOCALES) {
      const status = cellStatus(published, draft, spec.slug, locale);
      if (status !== "unchanged") cells.push({ slug: spec.slug, locale, status });
    }
  }
  return cells;
}

// --- word-level diff -------------------------------------------------------

export type DiffToken = { text: string; kind: "same" | "insert" | "delete" };

export function wordDiff(before: string, after: string): DiffToken[] {
  const a = before.split(/(\s+)/).filter((t) => t !== "");
  const b = after.split(/(\s+)/).filter((t) => t !== "");
  const n = a.length;
  const m = b.length;
  // Plain LCS. Prototype-grade: fine for a few thousand tokens.
  const table: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  const out: DiffToken[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ text: a[i], kind: "same" });
      i += 1;
      j += 1;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      out.push({ text: a[i], kind: "delete" });
      i += 1;
    } else {
      out.push({ text: b[j], kind: "insert" });
      j += 1;
    }
  }
  while (i < n) out.push({ text: a[i++], kind: "delete" });
  while (j < m) out.push({ text: b[j++], kind: "insert" });
  return out;
}

/** Collapse runs of unchanged text so a 280-line document shows only its edits. */
export function diffHunks(tokens: DiffToken[], context = 12) {
  const hunks: DiffToken[][] = [];
  let current: DiffToken[] = [];
  let sameRun: DiffToken[] = [];
  let open = false;
  for (const token of tokens) {
    if (token.kind === "same") {
      sameRun.push(token);
      if (open && sameRun.length > context * 2) {
        current.push(...sameRun.slice(0, context));
        hunks.push(current);
        current = [];
        sameRun = [];
        open = false;
      }
      continue;
    }
    if (!open) {
      current.push(...sameRun.slice(-context));
      open = true;
    } else {
      current.push(...sameRun);
    }
    sameRun = [];
    current.push(token);
  }
  if (open) {
    current.push(...sameRun.slice(0, context));
    hunks.push(current);
  }
  return hunks;
}

// --- the population the confirm step is about ------------------------------
//
// Real counts, read from the local database (a production copy) on 2026-08-31.
export const POPULATION = {
  customers: 1569,
  customersHoldingCurrentPolicy: 1058,
  staff: 25,
  staffHoldingCurrentTerms: 1,
};

export type PublishKind = "edition" | "correction";

export function regateCount(kind: PublishKind) {
  return kind === "edition" ? POPULATION.customersHoldingCurrentPolicy : 0;
}
