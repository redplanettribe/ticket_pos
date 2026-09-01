"use client";

import { useMemo } from "react";

import type { AppLocale } from "@ticket-pos/locale";
import { Badge, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import {
  diffCells,
  diffHunks,
  type ArtifactSet,
  type CellDiff,
  type DiffHunk,
  type DiffToken,
} from "@/lib/legal-drafts";

/**
 * What this draft CHANGES about the edition people are held to today (#562).
 *
 * THE UNIT IS THE (ARTIFACT × LOCALE) CELL, so a change to the Spanish policy is
 * never tangled with a change to an English checkbox label: they are two facts
 * about two different readers, and one summary covering both is how a paragraph
 * gets published that nobody meant to publish.
 *
 * THREE RULES ABOUT WHAT IS SHOWN, all of them about not burying the thing that
 * matters:
 *
 *   - AN ADDED OR REMOVED ARTIFACT COMES FIRST AND IS SHOWN WHOLE. It is not a
 *     wording change — somebody is now being asked for a consent they were not
 *     asked for, or has stopped being asked for one — and there is no unchanged
 *     run to collapse in text that is entirely new or entirely gone.
 *   - UNCHANGED CELLS ARE COLLAPSED BUT COUNTED. "12 of 14 cells unchanged" is a
 *     sentence an operator can check against their own memory; a wall of
 *     identical text is a sentence nobody reads.
 *   - INSIDE A DOCUMENT, LONG UNCHANGED RUNS COLLAPSE TO HUNKS WITH CONTEXT, and
 *     the elided lines are counted too. A two-word fix in a 280-line policy shows
 *     two words.
 *
 * NO LOGIC LIVES HERE. `diffCells`, `diffHunks` and `wordDiff` are in
 * lib/legal-drafts.ts and are unit-tested directly; this file decides only what
 * the change looks like on a screen. Colour is never the whole message — every
 * row carries a marker and a screen-reader word — because a red span alone is not
 * readable to everybody.
 */
export function LegalDiff({
  published,
  draft,
  locales,
  localeName,
}: {
  published: ArtifactSet;
  draft: ArtifactSet;
  locales: AppLocale[];
  localeName: (locale: AppLocale) => string;
}) {
  const t = useTranslations("operator.legal");

  const cells = useMemo(() => diffCells(published, draft, locales), [published, draft, locales]);
  const changed = cells.filter(
    (cell) => cell.status === "added" || cell.status === "removed" || cell.status === "modified",
  );
  const unchanged = cells.filter((cell) => cell.status === "unchanged");
  const missing = cells.filter((cell) => cell.status === "missing");

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {/* Collapsed, but COUNTED — and counted out of the whole grid, so the two
            numbers are checkable against each other. */}
        {t("diffUnchangedCells", { n: unchanged.length, total: cells.length })}
      </p>
      {missing.length > 0 ? (
        <p className="text-sm text-destructive">{t("diffMissingCells", { n: missing.length })}</p>
      ) : null}

      {changed.length === 0 ? (
        <p className="text-sm">{t("diffNoChanges")}</p>
      ) : (
        changed.map((cell) => (
          <CellDiffCard
            key={`${cell.slug}:${cell.locale}`}
            cell={cell}
            localeName={localeName}
          />
        ))
      )}
    </div>
  );
}

/** One cell's change: whole for a structural one, hunks for a rewording. */
function CellDiffCard({
  cell,
  localeName,
}: {
  cell: CellDiff;
  localeName: (locale: AppLocale) => string;
}) {
  const t = useTranslations("operator.legal");

  // Which side of a structural change this is, decided here rather than inline
  // so the two words stay out of the markup an i18n lint reads.
  const wholeOp: "added" | "removed" = cell.status === "added" ? "added" : "removed";

  const label =
    cell.status === "added"
      ? t("diffArtifactAdded")
      : cell.status === "removed"
        ? t("diffArtifactRemoved")
        : t("diffArtifactModified");

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle className="font-mono text-base">{cell.slug}</CardTitle>
            <CardDescription>{localeName(cell.locale)}</CardDescription>
          </div>
          <Badge variant={cell.status === "modified" ? "outline" : "default"}>{label}</Badge>
        </div>
      </CardHeader>
      <CardContent>
        {cell.status === "modified" ? (
          <HunkedDiff before={cell.before} after={cell.after} />
        ) : (
          // WHOLE, because there is nothing unchanged in it to collapse: this
          // text either did not exist yesterday or will not exist tomorrow.
          <WholeCell text={wholeOp === "added" ? cell.after : cell.before} op={wholeOp} />
        )}
      </CardContent>
    </Card>
  );
}

/** An added or removed artifact, every line of it. */
function WholeCell({ text, op }: { text: string; op: "added" | "removed" }) {
  const t = useTranslations("operator.legal");
  const marker = op === "added" ? "+" : "−";
  const word = op === "added" ? t("diffLineAdded") : t("diffLineRemoved");
  return (
    <pre className="overflow-x-auto rounded-md border p-3 font-mono text-xs whitespace-pre-wrap">
      {(text === "" ? [] : text.split("\n")).map((line, index) => (
        <div key={index} className={op === "added" ? "bg-primary/10" : "bg-destructive/10"}>
          <span aria-hidden className="mr-2 select-none text-muted-foreground">
            {marker}
          </span>
          <span className="sr-only">{word}</span>
          <span className={op === "removed" ? "line-through" : undefined}>{line || " "}</span>
        </div>
      ))}
    </pre>
  );
}

/** A reworded cell: hunks with context, and the elided lines counted. */
function HunkedDiff({ before, after }: { before: string; after: string }) {
  const t = useTranslations("operator.legal");
  const { hunks, unchangedLines } = useMemo(() => diffHunks(before, after), [before, after]);

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">{t("diffUnchangedLines", { n: unchangedLines })}</p>
      <div className="overflow-x-auto rounded-md border font-mono text-xs">
        {hunks.map((hunk, index) => (
          <Hunk key={index} hunk={hunk} />
        ))}
      </div>
    </div>
  );
}

function Hunk({ hunk }: { hunk: DiffHunk }) {
  const t = useTranslations("operator.legal");
  return (
    <div>
      {hunk.skippedBefore > 0 ? (
        // The collapse is SAID rather than silent: an operator who knows they
        // edited three places can count the hunks and the gaps between them.
        <div className="border-y bg-muted px-3 py-1 text-muted-foreground">
          {t("diffSkippedLines", { n: hunk.skippedBefore })}
        </div>
      ) : null}
      {hunk.rows.map((row, index) => (
        <div
          key={index}
          className={
            row.kind === "same"
              ? "px-3"
              : row.kind === "added"
                ? "bg-primary/10 px-3"
                : row.kind === "removed"
                  ? "bg-destructive/10 px-3"
                  : "bg-muted/60 px-3"
          }
        >
          <span aria-hidden className="mr-3 inline-block w-10 select-none text-right text-muted-foreground">
            {row.afterLine ?? row.beforeLine}
          </span>
          <span className="sr-only">
            {row.kind === "same"
              ? t("diffLineSame")
              : row.kind === "added"
                ? t("diffLineAdded")
                : row.kind === "removed"
                  ? t("diffLineRemoved")
                  : t("diffLineChanged")}
          </span>
          <span className="whitespace-pre-wrap">
            {row.tokens.map((token, tokenIndex) => (
              <Token key={tokenIndex} token={token} />
            ))}
          </span>
        </div>
      ))}
    </div>
  );
}

/** One run of words, and what happened to it. */
function Token({ token }: { token: DiffToken }) {
  if (token.op === "same") return <span>{token.text}</span>;
  if (token.op === "added") return <span className="rounded bg-primary/25">{token.text}</span>;
  return <span className="rounded bg-destructive/25 line-through">{token.text}</span>;
}
