import { LOCALES, type AppLocale } from "@ticket-pos/locale";

/**
 * The Legal Center's rules about a draft, as plain functions over plain data
 * (#561, spec #556).
 *
 * NO REACT AND NO NETWORK LIVES HERE, and that is the point of the file rather
 * than an accident of it. These functions encode the rulings that will REFUSE A
 * PUBLICATION — every artifact must exist in every published language; a change
 * to the artifact SET is structural and cannot be a correction — and a rule that
 * decides whether a legal document may be published is not something to verify
 * by rendering a component, clicking a textarea and reading a coloured dot back
 * out of the DOM. They are tested directly, in legal-drafts.test.ts, and the
 * components that consume them are not tested in their place.
 *
 * TOTAL FUNCTIONS. Every one of them answers for any input, including a slug
 * nothing knows about and a language nothing is written in, because an operator
 * adding an artifact invents both.
 *
 * #562 adds `wordDiff` and `diffHunks` here — the preview's diff is the same
 * kind of thing, pure over the same two ArtifactSets — and #563 reads
 * `completeness` and `isStructuralChange` to decide what the publish step is
 * allowed to offer.
 */

/** The two documents the Legal Center can draft. */
export type LegalDocumentKind = "policy" | "terms";

/**
 * A one-line checkbox label and a ~280-line legal document want different
 * affordances: a label gets a single-line box, the document gets a tall one.
 * NEITHER gets a rich-text editor — see the editor component for why.
 */
export type ArtifactSize = "line" | "document";

/**
 * slug → locale → body. A MISSING KEY MEANS THE ARTIFACT IS NOT WRITTEN IN THAT
 * LANGUAGE, and an empty string means the same thing: `cellStatus` collapses the
 * two deliberately, because "typed and then cleared" and "never typed" are the
 * same fact about a document and the API stores both as an absent row.
 *
 * The KEY ORDER IS THE ORDINAL ORDER — the fingerprint preimage's order (#541).
 * The API hands artifacts back in ordinal order and takes them back in list
 * order, stamping the ordinal from the position, so an ArtifactSet built by
 * walking that list carries the order in its keys and `draftSlugs` reads it back
 * out. Nothing here re-sorts.
 */
export type ArtifactSet = Record<string, Partial<Record<AppLocale, string>>>;

/** One artifact as the API sends it: a slug, its position, and its text per language. */
export type LegalArtifact = {
  slug: string;
  ordinal: number;
  bodies: Partial<Record<AppLocale, string>>;
};

/**
 * What a slug is, as far as the editor is concerned: where it sits, how big its
 * box is, and whether the platform has ever heard of it.
 *
 * IT CARRIES NO WORDS. The title an operator reads and the sentence saying where
 * the text appears in the wild are copy, and copy is translated (ADR 0041) — so
 * they are message keys resolved by the component, and this module stays free of
 * both React and language.
 */
export type SlugSpec = {
  slug: string;
  /** 1-based position in the draft, which IS the preimage ordinal. */
  ordinal: number;
  size: ArtifactSize;
  /**
   * False for a slug the platform does not render anywhere — an artifact the
   * operator invented in this draft. The editor says so, because publishing text
   * no surface asks for is a thing worth noticing before rather than after.
   */
  known: boolean;
};

/**
 * The artifact inventory (#558): the slugs the platform's surfaces actually ask
 * for, and how big each one's box is.
 *
 * THIS IS NOT THE ORDER OF ANY DRAFT. The order that matters is the draft's own,
 * because it is the preimage order and an operator may insert an artifact in the
 * middle of it. This table answers only "how big is this box, and does anything
 * render this slug".
 */
export const ARTIFACT_SIZES: Record<string, ArtifactSize> = {
  "short-notice": "document",
  "label-policy-acceptance": "line",
  "label-marketing-consent": "line",
  "label-networking-consent": "line",
  policy: "document",
  "label-terms-acceptance": "line",
  terms: "document",
};

export type CellStatus = "unchanged" | "modified" | "added" | "removed" | "missing";

/**
 * What has happened to one cell — one artifact in one language — between the
 * published edition and the draft.
 *
 * "missing" is not a kind of change, it is the ABSENCE of text where the draft
 * says there should be some, and it is what `completeness` counts. "removed" is
 * a change: the artifact was published in this language and the draft has taken
 * it out, which is structural.
 */
export function cellStatus(
  published: ArtifactSet,
  draft: ArtifactSet,
  slug: string,
  locale: AppLocale,
): CellStatus {
  const before = published[slug]?.[locale];
  const after = draft[slug]?.[locale];
  if (!before && !after) return "missing";
  if (!before) return after?.trim() ? "added" : "missing";
  if (!after) return "removed";
  if (!after.trim()) return "missing";
  return before.trim() === after.trim() ? "unchanged" : "modified";
}

/** One hole in a draft: an artifact with nothing written in a language it publishes. */
export type CompletenessGap = { slug: string; locale: AppLocale };

/**
 * Publish is refused unless every artifact exists in every published language.
 *
 * THE LANGUAGES ARE AN ARGUMENT, not a constant, and this is the one place this
 * module differs from the prototype it was lifted from. A draft carries an
 * EXPLICIT published-language set (#561) precisely so that a half-translated
 * language is a draft that cannot publish rather than a language quietly dropped
 * by an empty textarea — and a set that the caller states is a set the caller
 * must be able to state. It defaults to the platform's app locales, which is the
 * bound the set can never exceed.
 */
export function completeness(
  published: ArtifactSet,
  draft: ArtifactSet,
  locales: readonly AppLocale[] = LOCALES,
): { complete: boolean; gaps: CompletenessGap[] } {
  const gaps: CompletenessGap[] = [];
  for (const spec of draftSlugs(draft)) {
    for (const locale of locales) {
      if (cellStatus(published, draft, spec.slug, locale) === "missing") {
        gaps.push({ slug: spec.slug, locale });
      }
    }
  }
  return { complete: gaps.length === 0, gaps };
}

/**
 * True when the draft changes the artifact SET, not just its wording.
 *
 * This is the one structural change the code can PROVE is not a typo, and it is
 * what refuses the correction path: a correction says "the words were wrong", and
 * an edition that gained or lost an artifact is not the same document with better
 * words — somebody is now being asked for a consent they were not asked for, or
 * has stopped being asked for one.
 */
export function isStructuralChange(
  published: ArtifactSet,
  draft: ArtifactSet,
  locales: readonly AppLocale[] = LOCALES,
): boolean {
  // THE UNION OF BOTH SIDES, and this is the correction of a bug the prototype
  // could not have: an artifact the draft REMOVED is by definition not among the
  // draft's slugs, so walking the draft alone would report a deletion as an
  // ordinary rewording and let it be published as a correction. The prototype
  // walked a hard-coded published slug list plus its one invention, which hid
  // this; the real draft's slug list is the draft's own.
  return allSlugs(published, draft).some((slug) =>
    locales.some((locale) => {
      const status = cellStatus(published, draft, slug, locale);
      return status === "added" || status === "removed";
    }),
  );
}

/** Every slug either side names, published order first, then whatever the draft added. */
function allSlugs(published: ArtifactSet, draft: ArtifactSet): string[] {
  return [...new Set([...Object.keys(published), ...Object.keys(draft)])];
}

/**
 * The draft's artifacts, in order, each told what kind of box it needs and
 * whether anything renders it.
 *
 * THE ORDER IS THE DRAFT'S OWN and the ordinal is the position, so an artifact
 * inserted in the middle renumbers everything after it — which is correct,
 * because the ordinal is the preimage's order and inserting an artifact really
 * does change where the later ones sit in the hash.
 */
export function draftSlugs(draft: ArtifactSet): SlugSpec[] {
  return Object.keys(draft).map((slug, index) => ({
    slug,
    ordinal: index + 1,
    // A slug nobody has heard of gets the big box. A guess either way, and this
    // is the safe direction: a one-line label in a tall box is untidy, while a
    // page of prose in a one-line box is unusable.
    size: ARTIFACT_SIZES[slug] ?? "document",
    known: slug in ARTIFACT_SIZES,
  }));
}

/**
 * Whether the draft says anything different from the published edition at all —
 * what the editor asks before warning that a discard throws work away.
 */
export function hasChanges(
  published: ArtifactSet,
  draft: ArtifactSet,
  locales: readonly AppLocale[] = LOCALES,
): boolean {
  const slugs = allSlugs(published, draft);
  for (const slug of slugs) {
    for (const locale of locales) {
      const status = cellStatus(published, draft, slug, locale);
      if (status !== "unchanged" && status !== "missing") return true;
    }
  }
  return false;
}

/**
 * The API's artifact list turned into the editor's ArtifactSet, IN ORDER.
 *
 * The list arrives in ordinal order and object keys keep their insertion order,
 * so the ordinal survives the conversion without being carried as a field that
 * could disagree with it.
 */
export function toArtifactSet(artifacts: readonly LegalArtifact[]): ArtifactSet {
  const set: ArtifactSet = {};
  for (const artifact of artifacts) {
    set[artifact.slug] = { ...artifact.bodies };
  }
  return set;
}

/**
 * The editor's ArtifactSet turned back into what the save call takes: a list, in
 * order, with no ordinals on it. The API stamps the ordinal from the position,
 * so there is deliberately nothing here that could state a different one.
 */
export function toArtifactList(draft: ArtifactSet): { slug: string; bodies: Record<string, string> }[] {
  return Object.entries(draft).map(([slug, bodies]) => {
    const written: Record<string, string> = {};
    for (const [locale, body] of Object.entries(bodies)) {
      if (body !== undefined) written[locale] = body;
    }
    return { slug, bodies: written };
  });
}
