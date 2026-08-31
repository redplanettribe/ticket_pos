/* eslint-disable i18next/no-literal-string */
// PROTOTYPE — throwaway. Variant A for issue #543.
//
// Thesis: the invariant that bites is "every artifact in every published
// language, complete". So put the languages side by side and never let the
// operator see one language alone. Publish is a rail that fills up.
"use client";

import { useMemo, useState } from "react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
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
  completeness,
  changedCells,
  draftSlugs,
  isStructuralChange,
  type ArtifactSet,
  type Locale,
  type PublishKind,
} from "./fixture";
import { PrototypeNote, StatusBadge, WordDiff } from "./prototype-kit";

export function VariantA({ draft, setDraft }: { draft: ArtifactSet; setDraft: (next: ArtifactSet) => void }) {
  const specs = draftSlugs(draft);
  const [active, setActive] = useState(specs[0].slug);
  const [focusLocale, setFocusLocale] = useState<Locale | null>(null);
  const [preview, setPreview] = useState(false);
  const [seenPreview, setSeenPreview] = useState<Set<string>>(new Set());
  const [seenDiff, setSeenDiff] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);
  const [confirmKind, setConfirmKind] = useState<PublishKind | null>(null);

  const spec = specs.find((s) => s.slug === active) ?? specs[0];
  const gaps = completeness(PUBLISHED.policy, draft);
  const changes = changedCells(PUBLISHED.policy, draft);
  const structural = isStructuralChange(PUBLISHED.policy, draft);
  const previewedAll = specs.every((s) => seenPreview.has(s.slug));
  const canPublish = gaps.complete && previewedAll && seenDiff;

  const edit = (slug: string, locale: Locale, value: string) => {
    setDraft({ ...draft, [slug]: { ...draft[slug], [locale]: value } });
  };

  const shownLocales = useMemo(() => (focusLocale ? [focusLocale] : LOCALES), [focusLocale]);

  return (
    <div className="grid grid-cols-[220px_minmax(0,1fr)_280px] gap-6">
      {/* Outline: every artifact, with a dot per language. The gap is visible from here. */}
      <nav className="space-y-1">
        <p className="px-2 pb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Privacy Policy — draft
        </p>
        {specs.map((s) => (
          <button
            key={s.slug}
            type="button"
            onClick={() => {
              setActive(s.slug);
              setPreview(false);
            }}
            className={cn(
              "w-full rounded-md px-2 py-2 text-left text-sm",
              s.slug === active ? "bg-accent font-medium" : "hover:bg-muted",
            )}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="truncate">
                <span className="mr-1.5 font-mono text-xs text-muted-foreground">{s.ordinal}</span>
                {s.title}
              </span>
              <span className="flex gap-1">
                {LOCALES.map((locale) => {
                  const status = cellStatus(PUBLISHED.policy, draft, s.slug, locale);
                  return (
                    <span
                      key={locale}
                      title={`${LOCALE_NAME[locale]}: ${status}`}
                      className={cn(
                        "inline-block size-2 rounded-full",
                        status === "missing" && "bg-rose-600",
                        status === "modified" && "bg-amber-500",
                        status === "added" && "bg-emerald-500",
                        status === "unchanged" && "bg-muted-foreground/30",
                      )}
                    />
                  );
                })}
              </span>
            </span>
          </button>
        ))}
        <PrototypeNote>
          The dots are the completeness rule made ambient: red on the right dot means the Spanish is missing.
        </PrototypeNote>
      </nav>

      {/* The artifact, both languages at once. */}
      <div className="min-w-0 space-y-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <div>
            <h2 className="text-lg font-semibold">{spec.title}</h2>
            <p className="text-sm text-muted-foreground">{spec.surface}</p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant={preview ? "default" : "outline"} size="sm" onClick={() => {
              setPreview((p) => !p);
              setSeenPreview((prev) => new Set(prev).add(spec.slug));
            }}>
              {preview ? "Editing" : "Preview"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setFocusLocale(focusLocale ? null : "es")}
            >
              {focusLocale ? "Both languages" : "Focus one"}
            </Button>
          </div>
        </div>

        <div className={cn("grid gap-4", shownLocales.length === 2 ? "grid-cols-2" : "grid-cols-1")}>
          {shownLocales.map((locale) => {
            const status = cellStatus(PUBLISHED.policy, draft, spec.slug, locale);
            const value = draft[spec.slug]?.[locale] ?? "";
            return (
              <div key={locale} className="min-w-0 space-y-2">
                <div className="flex items-center gap-2">
                  <Label className="text-xs uppercase tracking-wide">{LOCALE_NAME[locale]}</Label>
                  <StatusBadge status={status} />
                  {focusLocale && (
                    <button
                      type="button"
                      className="ml-auto text-xs underline"
                      onClick={() => setFocusLocale(locale === "en" ? "es" : "en")}
                    >
                      switch to {LOCALE_NAME[locale === "en" ? "es" : "en"]}
                    </button>
                  )}
                </div>
                {preview ? (
                  <div className="min-h-40 rounded-md border bg-background p-4">
                    {value ? <Markdown>{value}</Markdown> : <p className="text-sm text-rose-600">Not written.</p>}
                  </div>
                ) : (
                  <Textarea
                    value={value}
                    onChange={(event) => edit(spec.slug, locale, event.target.value)}
                    className={cn("font-mono text-xs leading-relaxed", spec.size === "document" ? "h-[28rem]" : "h-32")}
                    placeholder={`Write the ${LOCALE_NAME[locale]} text…`}
                  />
                )}
              </div>
            );
          })}
        </div>
        <PrototypeNote>
          Artifacts are authored one line per block (remark-breaks turns a wrapped line into a &lt;br&gt;), so the
          editor is a plain monospace box, not a rich text field.
        </PrototypeNote>
      </div>

      {/* The publish rail. Nothing here is a surprise at confirm time. */}
      <aside className="space-y-3">
        <Card>
          <CardContent className="space-y-3 p-4">
            <p className="text-sm font-semibold">Before publishing</p>
            <Check done={gaps.complete} label={`Complete in both languages${gaps.complete ? "" : ` — ${gaps.gaps.length} missing`}`} />
            <Check done={previewedAll} label={`Every artifact previewed (${seenPreview.size}/${specs.length})`} />
            <Check done={seenDiff} label="Diff against edition 1 reviewed" />
            <Button className="w-full" variant="outline" size="sm" onClick={() => { setDiffOpen(true); setSeenDiff(true); }}>
              Review the diff ({changes.length} changed)
            </Button>
            <div className="border-t pt-3">
              <Button className="w-full" disabled={!canPublish} onClick={() => setConfirmKind("edition")}>
                Publish new edition
              </Button>
              <p className="mt-1 text-center text-xs text-muted-foreground">
                {POPULATION.customersHoldingCurrentPolicy.toLocaleString()} people re-accept at next sign-in
              </p>
              <button
                type="button"
                disabled={!canPublish || structural}
                onClick={() => setConfirmKind("correction")}
                className="mt-3 w-full text-center text-xs text-muted-foreground underline disabled:no-underline disabled:opacity-50"
              >
                {structural
                  ? "A correction cannot add or remove an artifact"
                  : "This is a correction — publish without re-gating"}
              </button>
            </div>
          </CardContent>
        </Card>
        {!gaps.complete && (
          <Alert>
            <AlertTitle>Not publishable yet</AlertTitle>
            <AlertDescription>
              <ul className="mt-1 space-y-1 text-xs">
                {gaps.gaps.map((gap) => (
                  <li key={`${gap.slug}-${gap.locale}`}>
                    {gap.slug} — {LOCALE_NAME[gap.locale]}
                  </li>
                ))}
              </ul>
            </AlertDescription>
          </Alert>
        )}
      </aside>

      <DiffDialog open={diffOpen} onOpenChange={setDiffOpen} draft={draft} />
      <ConfirmDialog kind={confirmKind} onClose={() => setConfirmKind(null)} draft={draft} />
    </div>
  );
}

function Check({ done, label }: { done: boolean; label: string }) {
  return (
    <p className={cn("flex items-start gap-2 text-xs", done ? "text-foreground" : "text-muted-foreground")}>
      <span className={cn("mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[10px]", done ? "bg-emerald-600 text-white" : "border")}>
        {done ? "✓" : ""}
      </span>
      {label}
    </p>
  );
}

function DiffDialog({ open, onOpenChange, draft }: { open: boolean; onOpenChange: (v: boolean) => void; draft: ArtifactSet }) {
  const specs = draftSlugs(draft);
  const [expanded, setExpanded] = useState<string | null>(null);
  const changes = changedCells(PUBLISHED.policy, draft);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-4xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Edition 2 draft vs. edition 1</DialogTitle>
          <DialogDescription>
            {changes.length} of {specs.length * LOCALES.length} cells changed. Structural changes are listed first —
            adding an artifact shifts the ordinals that the fingerprint is computed over.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          {specs.map((spec) =>
            LOCALES.map((locale) => {
              const status = cellStatus(PUBLISHED.policy, draft, spec.slug, locale);
              const key = `${spec.slug}:${locale}`;
              const before = PUBLISHED.policy[spec.slug]?.[locale] ?? "";
              const after = draft[spec.slug]?.[locale] ?? "";
              return (
                <div key={key} className={cn("rounded-md border", status === "unchanged" && "opacity-60")}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm"
                    onClick={() => setExpanded(expanded === key ? null : key)}
                  >
                    <StatusBadge status={status} />
                    <span className="font-medium">{spec.title}</span>
                    <span className="text-muted-foreground">{LOCALE_NAME[locale]}</span>
                    <span className="ml-auto text-xs text-muted-foreground">{expanded === key ? "hide" : "show"}</span>
                  </button>
                  {expanded === key && (
                    <div className="border-t p-3">
                      {status === "added" ? (
                        <div className="space-y-2">
                          <Badge className="bg-emerald-100 text-emerald-900">New artifact — shown whole</Badge>
                          <div className="rounded-md border bg-emerald-50 p-3 text-sm">{after || "Not written."}</div>
                        </div>
                      ) : (
                        <WordDiff before={before} after={after} />
                      )}
                    </div>
                  )}
                </div>
              );
            }),
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ConfirmDialog({ kind, onClose, draft }: { kind: PublishKind | null; onClose: () => void; draft: ArtifactSet }) {
  const [reason, setReason] = useState("");
  const [when, setWhen] = useState("");
  const changes = changedCells(PUBLISHED.policy, draft);
  const scheduled = when.trim() !== "";

  return (
    <Dialog open={kind !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{kind === "correction" ? "Publish a correction" : "Publish edition 2"}</DialogTitle>
          <DialogDescription>
            {kind === "correction" ? (
              <>
                <strong>Nobody is re-gated.</strong> Everyone who accepted edition 1 stays accepted, and the text they
                are held to changes underneath them.
              </>
            ) : (
              <>
                <strong>{POPULATION.customersHoldingCurrentPolicy.toLocaleString()} customers</strong> will be asked to
                accept again the next time they sign in. {(POPULATION.customers - POPULATION.customersHoldingCurrentPolicy).toLocaleString()}{" "}
                more have never accepted and are already outstanding.
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          <p className="text-muted-foreground">{changes.length} cells changed across 2 languages.</p>

          {kind === "correction" && (
            <div className="space-y-2">
              <Label htmlFor="reason">Why is this a correction and not a new edition?</Label>
              <Textarea
                id="reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="Recorded against your name, permanently."
                className="h-24"
              />
            </div>
          )}

          <div className="space-y-2">
            <Label htmlFor="when">Effective date</Label>
            <div className="flex items-center gap-2">
              <Input id="when" type="date" value={when} onChange={(event) => setWhen(event.target.value)} className="w-44" />
              <span className="text-xs text-muted-foreground">
                {scheduled ? "00:00 America/Guayaquil — cancellable until then" : "Leave empty to take effect now"}
              </span>
            </div>
          </div>

          <PrototypeNote>
            Effective date lives here, not in the editor: it is a property of the act of publishing, not of the text.
          </PrototypeNote>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant={kind === "correction" ? "outline" : "default"}
            disabled={kind === "correction" && reason.trim().length < 12}
            onClick={onClose}
          >
            {kind === "correction"
              ? "Publish correction — re-gate nobody"
              : scheduled
                ? `Schedule for ${when}`
                : `Publish and re-gate ${POPULATION.customersHoldingCurrentPolicy.toLocaleString()}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
