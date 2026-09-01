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
  // The Adulthood Declaration's own checkbox label (ADR 0069). It is here for
  // the same reason every other slug is — so an operator authoring it meets a
  // one-line box and not a wall of textarea — and for no other: nothing in this
  // module, or anywhere else in the publish path, knows that this particular
  // label is about being eighteen. Adding it to a draft is a change to the
  // artifact SET and therefore structural, and dropping it again is too,
  // BECAUSE OF THE GENERAL RULE and not because of an entry in this table.
  "label-adulthood-declaration": "line",
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

/* ==========================================================================
 * The diff (#562)
 *
 * What the operator is shown before they are allowed to publish: what this
 * draft CHANGES about the edition people are held to today. It lives here, with
 * the rest of the rules, and not in the component that draws it, because it is
 * the same kind of thing as `completeness` — a total function over two
 * ArtifactSets — and because a diff nobody can unit-test is a diff nobody can
 * trust, while what it is FOR is trust.
 *
 * THE UNIT IS THE (ARTIFACT × LOCALE) CELL. Not the document and not the
 * artifact: a change to the Spanish policy and a change to an English checkbox
 * label are two different facts about two different readers, and tangling them
 * into one "the policy changed" is exactly the kind of summary that gets a
 * paragraph published nobody meant to publish.
 * ========================================================================== */

/** What happened to one run of words. */
export type DiffOp = "same" | "added" | "removed";

/**
 * One run of words, all of which had the same thing happen to them. Runs are
 * merged, so a rewritten sentence is one `removed` token and one `added` token
 * rather than forty of each.
 */
export type DiffToken = { op: DiffOp; text: string };

/**
 * The word-level diff of two pieces of text, by LONGEST COMMON SUBSEQUENCE.
 *
 * WORD-LEVEL AND NOT LINE-LEVEL, because these documents are ~280 lines of
 * prose where a correction is usually two words inside one of them. A line diff
 * would show the whole paragraph struck out and the whole paragraph added back,
 * and an operator asked to check that would be checking nothing — the entire
 * value of the screen is that a two-word fix LOOKS like a two-word fix.
 *
 * WHITESPACE IS TOKENISED TOO, and kept in the tokens, so that rejoining every
 * token's text reproduces the input exactly. That matters more here than in an
 * ordinary diff: the renderer runs `remark-breaks`, so a newline is a <br> in
 * the published document, and a diff that quietly normalised whitespace would
 * hide the one class of edit that changes the layout of a legal document
 * without changing a word of it.
 *
 * TOTAL, including for text nobody would write: either side empty, both empty,
 * and text far too long to diff — see the guard below, which degrades to "all
 * of this went, all of that arrived" rather than to a hung tab.
 */
export function wordDiff(before: string, after: string): DiffToken[] {
  return mergeTokens(diffSequences(tokenize(before), tokenize(after)));
}

/** Words and whitespace runs, in order, losslessly. */
function tokenize(text: string): string[] {
  return text.match(/\s+|\S+/g) ?? [];
}

/**
 * The most tokens either side may carry before the LCS table is abandoned.
 *
 * The table is O(n×m) cells, and this module runs in a browser tab holding a
 * legal document. A cell that exceeds it is reported as a wholesale replacement,
 * which is honest — every word did change position — and is what a diff of two
 * unrelated texts looks like anyway. `diffHunks` feeds this function ONE LINE at
 * a time, so nothing an operator actually writes comes near the bound.
 */
const MAX_DIFF_TOKENS = 2500;

/** The LCS walk itself, over any two sequences of comparable strings. */
function diffSequences(before: readonly string[], after: readonly string[]): DiffToken[] {
  // Common ends are matched off first. It is the difference between diffing a
  // paragraph and diffing the two words inside it that moved, and it is what
  // keeps the table below small on the edits people actually make.
  let head = 0;
  while (head < before.length && head < after.length && before[head] === after[head]) head += 1;
  let tail = 0;
  while (
    tail < before.length - head &&
    tail < after.length - head &&
    before[before.length - 1 - tail] === after[after.length - 1 - tail]
  ) {
    tail += 1;
  }

  const middleBefore = before.slice(head, before.length - tail);
  const middleAfter = after.slice(head, after.length - tail);

  const tokens: DiffToken[] = [];
  for (const text of before.slice(0, head)) tokens.push({ op: "same", text });

  if (middleBefore.length > MAX_DIFF_TOKENS || middleAfter.length > MAX_DIFF_TOKENS) {
    for (const text of middleBefore) tokens.push({ op: "removed", text });
    for (const text of middleAfter) tokens.push({ op: "added", text });
  } else {
    tokens.push(...lcsWalk(middleBefore, middleAfter));
  }

  for (const text of before.slice(before.length - tail)) tokens.push({ op: "same", text });
  return tokens;
}

/**
 * The classic LCS dynamic program, walked forwards into edit operations.
 *
 * `lengths[i][j]` is the length of the longest common subsequence of the
 * suffixes starting at i and j, so the forward walk can always pick the branch
 * that keeps the most in common. Ties go to REMOVED first, which is what puts a
 * replaced phrase's old words before its new ones — the order a person reads a
 * correction in.
 */
function lcsWalk(before: readonly string[], after: readonly string[]): DiffToken[] {
  const columns = after.length + 1;
  const lengths = new Uint32Array((before.length + 1) * columns);
  for (let i = before.length - 1; i >= 0; i -= 1) {
    for (let j = after.length - 1; j >= 0; j -= 1) {
      lengths[i * columns + j] =
        before[i] === after[j]
          ? lengths[(i + 1) * columns + j + 1] + 1
          : Math.max(lengths[(i + 1) * columns + j], lengths[i * columns + j + 1]);
    }
  }

  const tokens: DiffToken[] = [];
  let i = 0;
  let j = 0;
  while (i < before.length && j < after.length) {
    if (before[i] === after[j]) {
      tokens.push({ op: "same", text: before[i] });
      i += 1;
      j += 1;
    } else if (lengths[(i + 1) * columns + j] >= lengths[i * columns + j + 1]) {
      tokens.push({ op: "removed", text: before[i] });
      i += 1;
    } else {
      tokens.push({ op: "added", text: after[j] });
      j += 1;
    }
  }
  while (i < before.length) {
    tokens.push({ op: "removed", text: before[i] });
    i += 1;
  }
  while (j < after.length) {
    tokens.push({ op: "added", text: after[j] });
    j += 1;
  }
  return tokens;
}

/** Adjacent runs of one op become one token, so the rendering is spans and not confetti. */
function mergeTokens(tokens: readonly DiffToken[]): DiffToken[] {
  const merged: DiffToken[] = [];
  for (const token of tokens) {
    const last = merged[merged.length - 1];
    if (last && last.op === token.op) {
      last.text += token.text;
      continue;
    }
    merged.push({ ...token });
  }
  return merged;
}

/** What happened to one line of one cell. */
export type DiffRowKind = "same" | "added" | "removed" | "changed";

/**
 * One line of a cell's diff. `changed` is a line that exists on both sides with
 * different words in it, and it is the only kind whose tokens are worth reading
 * closely — the other three carry a single token holding the whole line, so a
 * renderer can treat every row the same way.
 *
 * The line NUMBERS are 1-based and are the numbers on each side, which differ
 * once anything has been inserted. Null means the line is not on that side.
 */
export type DiffRow = {
  kind: DiffRowKind;
  beforeLine: number | null;
  afterLine: number | null;
  tokens: DiffToken[];
};

/** A run of rows worth showing, and how many unchanged lines were skipped to reach it. */
export type DiffHunk = {
  /** Unchanged lines elided immediately before this hunk. Zero for a hunk that starts at the top. */
  skippedBefore: number;
  rows: DiffRow[];
};

/**
 * One cell's diff, as hunks with context.
 *
 * WHY HUNKS. The Privacy Policy is ~280 lines. A two-word fix in it must show
 * two words, not 280 lines with two of them highlighted somewhere in the middle
 * — an operator scrolling a wall of unchanged text to look for a change is an
 * operator who will stop looking. Long unchanged runs collapse, and how many
 * lines went with them is REPORTED rather than silently dropped, because "42
 * unchanged lines" is a fact the operator can check against their own memory of
 * what they edited.
 *
 * `context` lines of unchanged text survive on each side of a change, because a
 * changed line with nothing around it is a sentence out of context and the
 * question being asked is whether it reads correctly IN the document.
 *
 * A removed line and an added line that meet at the same place are paired into
 * one `changed` row and diffed word by word. That pairing is a heuristic and
 * this is it stated plainly: within a block of edits the n-th removal is shown
 * against the n-th addition, and whatever is left over stays a whole removed or
 * whole added line. It is what makes an edited paragraph read as an edit; it can
 * pair two lines that have nothing to do with each other, in which case the row
 * degenerates to "all of this went, all of that arrived" — which is what the
 * unpaired rows would have shown anyway.
 */
export function diffHunks(
  before: string,
  after: string,
  context = 3,
): { hunks: DiffHunk[]; unchangedLines: number } {
  const rows = pairChangedLines(diffSequences(splitLines(before), splitLines(after)));
  const unchangedLines = rows.filter((row) => row.kind === "same").length;

  // Which rows are near enough to a change to be worth showing.
  const keep = rows.map((row) => row.kind !== "same");
  for (let index = 0; index < rows.length; index += 1) {
    if (rows[index].kind === "same") continue;
    for (let offset = 1; offset <= context; offset += 1) {
      if (index - offset >= 0) keep[index - offset] = true;
      if (index + offset < rows.length) keep[index + offset] = true;
    }
  }

  const hunks: DiffHunk[] = [];
  let skipped = 0;
  let current: DiffHunk | null = null;
  for (let index = 0; index < rows.length; index += 1) {
    if (!keep[index]) {
      skipped += 1;
      current = null;
      continue;
    }
    if (!current) {
      current = { skippedBefore: skipped, rows: [] };
      hunks.push(current);
      skipped = 0;
    }
    current.rows.push(rows[index]);
  }
  return { hunks, unchangedLines };
}

/**
 * Lines, with an empty string meaning NO LINES rather than one empty one.
 *
 * `"".split("\n")` is `[""]`, which would make an empty cell diff as a document
 * containing one blank line — and an empty cell is a cell nobody has written.
 */
function splitLines(text: string): string[] {
  return text === "" ? [] : text.split("\n");
}

/** The line-level ops turned into rows, pairing replacements as they go. */
function pairChangedLines(ops: readonly DiffToken[]): DiffRow[] {
  const rows: DiffRow[] = [];
  let beforeLine = 0;
  let afterLine = 0;

  for (let index = 0; index < ops.length; ) {
    if (ops[index].op === "same") {
      beforeLine += 1;
      afterLine += 1;
      rows.push({ kind: "same", beforeLine, afterLine, tokens: [{ op: "same", text: ops[index].text }] });
      index += 1;
      continue;
    }

    // One block of edits: everything removed here, then everything added here.
    const removed: string[] = [];
    const added: string[] = [];
    while (index < ops.length && ops[index].op === "removed") {
      removed.push(ops[index].text);
      index += 1;
    }
    while (index < ops.length && ops[index].op === "added") {
      added.push(ops[index].text);
      index += 1;
    }

    const paired = Math.min(removed.length, added.length);
    for (let n = 0; n < paired; n += 1) {
      beforeLine += 1;
      afterLine += 1;
      rows.push({ kind: "changed", beforeLine, afterLine, tokens: wordDiff(removed[n], added[n]) });
    }
    for (const text of removed.slice(paired)) {
      beforeLine += 1;
      rows.push({ kind: "removed", beforeLine, afterLine: null, tokens: [{ op: "removed", text }] });
    }
    for (const text of added.slice(paired)) {
      afterLine += 1;
      rows.push({ kind: "added", beforeLine: null, afterLine, tokens: [{ op: "added", text }] });
    }
  }
  return rows;
}

/** One cell of the diff: one artifact, in one language, on both sides. */
export type CellDiff = {
  slug: string;
  locale: AppLocale;
  status: CellStatus;
  /** The published text, or "" where the artifact is new. */
  before: string;
  /** The draft's text, or "" where the artifact has been taken out. */
  after: string;
};

/**
 * Every cell of the diff, SORTED SO THE STRUCTURAL CHANGES COME FIRST.
 *
 * An artifact that appeared or disappeared is not a wording change and must not
 * be read as one: somebody is now being asked for a consent they were not asked
 * for, or has stopped being asked for one. Buried three screens down among comma
 * fixes it would be missed, so it sorts to the top and the component renders it
 * WHOLE rather than as hunks — there is no "unchanged run" to collapse in text
 * that is entirely new or entirely gone.
 *
 * Unchanged cells sort last and are the ones the caller collapses; they are
 * RETURNED rather than dropped so that the count is the list's own length and
 * "12 of 12 unchanged" cannot drift from what is on screen.
 */
export function diffCells(
  published: ArtifactSet,
  draft: ArtifactSet,
  locales: readonly AppLocale[] = LOCALES,
): CellDiff[] {
  const cells: CellDiff[] = [];
  for (const slug of allSlugs(published, draft)) {
    for (const locale of locales) {
      cells.push({
        slug,
        locale,
        status: cellStatus(published, draft, slug, locale),
        before: published[slug]?.[locale] ?? "",
        after: draft[slug]?.[locale] ?? "",
      });
    }
  }
  // A STABLE sort over the union order built above, so two renders of one draft
  // put the cells in the same places and an operator's eye can go back to where
  // it was.
  const rank: Record<CellStatus, number> = { added: 0, removed: 0, modified: 1, missing: 2, unchanged: 3 };
  return cells.sort((a, b) => rank[a.status] - rank[b.status]);
}
