/* eslint-disable i18next/no-literal-string */
// PROTOTYPE — throwaway. Variant C for issue #543.
//
// Thesis: the editor IS the diff. An edition is never authored from nothing —
// it is always the current edition, changed. So show published-vs-draft for
// every cell and let the operator type into the right-hand side. Publishing is
// a sticky impact bar, not a modal.
"use client";

import { useState } from "react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  Markdown,
  Textarea,
  cn,
} from "@ticket-pos/ui";

import {
  LOCALES,
  LOCALE_NAME,
  POPULATION,
  PUBLISHED,
  cellStatus,
  changedCells,
  completeness,
  draftSlugs,
  isStructuralChange,
  type ArtifactSet,

} from "./fixture";
import { PrototypeNote, StatusBadge, WordDiff } from "./prototype-kit";

export function VariantC({ draft, setDraft }: { draft: ArtifactSet; setDraft: (next: ArtifactSet) => void }) {
  const [showUnchanged, setShowUnchanged] = useState(false);
  const [openCell, setOpenCell] = useState<string | null>("short-notice:en");
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [correctionOpen, setCorrectionOpen] = useState(false);
  const [scheduled, setScheduled] = useState<string | null>("2026-09-15");

  const specs = draftSlugs(draft);
  const gaps = completeness(PUBLISHED.policy, draft);
  const changes = changedCells(PUBLISHED.policy, draft);
  const structural = isStructuralChange(PUBLISHED.policy, draft);

  const rows = specs.flatMap((spec) =>
    LOCALES.map((locale) => ({ spec, locale, status: cellStatus(PUBLISHED.policy, draft, spec.slug, locale) })),
  );
  const visible = rows.filter((row) => showUnchanged || row.status !== "unchanged");

  return (
    <div className="space-y-4 pb-28">
      {scheduled && (
        <Alert className="border-sky-300 bg-sky-50">
          <AlertTitle className="flex items-center justify-between gap-4">
            <span>Edition 2 is scheduled for {scheduled}</span>
            <Button size="sm" variant="outline" onClick={() => setScheduled(null)}>
              Cancel before it takes effect
            </Button>
          </AlertTitle>
          <AlertDescription className="text-xs">
            Takes effect at 00:00 America/Guayaquil. Until then the public pages still serve edition 1. Cancelling keeps
            the row and marks it cancelled — nothing is deleted.
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">Privacy Policy — draft against edition 1</h2>
          <p className="text-sm text-muted-foreground">
            {changes.length} of {rows.length} cells changed · {specs.length} artifacts × {LOCALES.length} languages
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={() => setShowUnchanged((value) => !value)}>
          {showUnchanged ? `Hide the ${rows.length - changes.length} unchanged` : `Show all ${rows.length} cells`}
        </Button>
      </div>

      <PrototypeNote>
        Unchanged cells are hidden but counted — the operator can always see that 6 of 12 were left alone, which is the
        thing a reviewer actually wants to know.
      </PrototypeNote>

      <div className="space-y-2">
        {visible.map(({ spec, locale, status }) => {
          const key = `${spec.slug}:${locale}`;
          const before = PUBLISHED.policy[spec.slug]?.[locale] ?? "";
          const after = draft[spec.slug]?.[locale] ?? "";
          const open = openCell === key;
          return (
            <div key={key} className={cn("rounded-md border", status === "missing" && "border-rose-400")}>
              <button
                type="button"
                onClick={() => setOpenCell(open ? null : key)}
                className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm"
              >
                <span className="font-mono text-xs text-muted-foreground">{spec.ordinal}</span>
                <span className="font-medium">{spec.title}</span>
                <span className="text-muted-foreground">{LOCALE_NAME[locale]}</span>
                <StatusBadge status={status} className="ml-auto" />
              </button>
              {open && (
                <div className="grid grid-cols-2 gap-0 border-t">
                  <div className="border-r p-3">
                    <p className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">
                      Edition 1 — published, immutable
                    </p>
                    {before ? (
                      <div className="max-h-80 overflow-y-auto rounded-md bg-muted/40 p-3">
                        <Markdown>{before}</Markdown>
                      </div>
                    ) : (
                      <p className="text-sm text-muted-foreground">Did not exist in edition 1.</p>
                    )}
                  </div>
                  <div className="space-y-2 p-3">
                    <p className="text-xs uppercase tracking-wide text-muted-foreground">Draft — editable</p>
                    <Textarea
                      value={after}
                      onChange={(event) => setDraft({ ...draft, [spec.slug]: { ...draft[spec.slug], [locale]: event.target.value } })}
                      className={cn("font-mono text-xs leading-relaxed", spec.size === "document" ? "h-72" : "h-24")}
                      placeholder={`Write the ${LOCALE_NAME[locale]} text…`}
                    />
                    {status === "modified" && (
                      <div className="pt-1">
                        <p className="mb-1 text-xs uppercase tracking-wide text-muted-foreground">Word diff</p>
                        <WordDiff before={before} after={after} />
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Sticky impact bar. The number is the headline, not a line of small print in a modal. */}
      <div className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 backdrop-blur">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-4 px-6 py-3">
          <div className="min-w-0">
            <p className="text-sm font-medium">
              {gaps.complete
                ? `Publishing re-gates ${POPULATION.customersHoldingCurrentPolicy.toLocaleString()} customers`
                : `${gaps.gaps.length} artifact(s) not written — publish is refused`}
            </p>
            <p className="text-xs text-muted-foreground">
              {changes.length} changed cells{structural ? " · adds an artifact" : ""} ·{" "}
              {(POPULATION.customers - POPULATION.customersHoldingCurrentPolicy).toLocaleString()} already outstanding
            </p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              disabled={structural || !gaps.complete}
              onClick={() => setCorrectionOpen(true)}
              className="text-xs text-muted-foreground underline disabled:no-underline disabled:opacity-40"
            >
              correction…
            </button>
            <Button variant="outline" disabled={!gaps.complete} onClick={() => setScheduleOpen(true)}>
              Schedule…
            </Button>
            <Button disabled={!gaps.complete}>Publish now</Button>
          </div>
        </div>
      </div>

      <Dialog open={scheduleOpen} onOpenChange={setScheduleOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Schedule edition 2</DialogTitle>
            <DialogDescription>
              It becomes the current edition at 00:00 America/Guayaquil on that date, with no request needed to trigger
              it. Cancellable until then.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="c-when">Effective date</Label>
            <Input id="c-when" type="date" defaultValue="2026-09-15" className="w-44" />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setScheduleOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => setScheduleOpen(false)}>Schedule</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={correctionOpen} onOpenChange={setCorrectionOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Publish as a correction</DialogTitle>
            <DialogDescription>
              A correction re-gates <strong>nobody</strong>. The {POPULATION.customersHoldingCurrentPolicy.toLocaleString()}{" "}
              people holding edition 1 will be held to the changed text without being shown it again. Use this only when
              the meaning has not changed.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="c-reason">Why is this not a new edition?</Label>
            <Textarea id="c-reason" className="h-24" placeholder="Recorded against your name, permanently." />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCorrectionOpen(false)}>
              Back
            </Button>
            <Button variant="outline" onClick={() => setCorrectionOpen(false)}>
              Publish correction
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
