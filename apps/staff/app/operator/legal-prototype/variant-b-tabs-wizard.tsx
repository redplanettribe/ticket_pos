/* eslint-disable i18next/no-literal-string */
// PROTOTYPE — throwaway. Variant B for issue #543.
//
// Thesis: one language at a time, full width, all artifacts stacked in ordinal
// order — the operator reads the edition the way a reader will. Publishing is a
// linear wizard that cannot be skipped: complete → preview → diff → confirm.
"use client";

import { useState } from "react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
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
  changedCells,
  completeness,
  draftSlugs,
  isStructuralChange,
  type ArtifactSet,
  type Locale,
  type PublishKind,
} from "./fixture";
import { PrototypeNote, StatusBadge, WordDiff } from "./prototype-kit";

export function VariantB({ draft, setDraft }: { draft: ArtifactSet; setDraft: (next: ArtifactSet) => void }) {
  const [locale, setLocale] = useState<Locale>("es");
  const [wizardOpen, setWizardOpen] = useState(false);
  const specs = draftSlugs(draft);
  const gaps = completeness(PUBLISHED.policy, draft);

  const gapsFor = (target: Locale) => gaps.gaps.filter((gap) => gap.locale === target).length;

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <div className="flex items-center justify-between gap-4 border-b">
        <div className="flex">
          {LOCALES.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setLocale(option)}
              className={cn(
                "flex items-center gap-2 border-b-2 px-4 py-2 text-sm",
                option === locale ? "border-foreground font-medium" : "border-transparent text-muted-foreground",
              )}
            >
              {LOCALE_NAME[option]}
              {gapsFor(option) > 0 && (
                <span className="rounded-full bg-rose-600 px-1.5 text-[11px] text-white">{gapsFor(option)}</span>
              )}
            </button>
          ))}
        </div>
        <Button size="sm" onClick={() => setWizardOpen(true)}>
          Publish…
        </Button>
      </div>

      <PrototypeNote>
        The tab badge is the only thing standing between the operator and publishing a language that has fallen behind.
        Variant A makes the same gap ambient instead.
      </PrototypeNote>

      {specs.map((spec) => {
        const status = cellStatus(PUBLISHED.policy, draft, spec.slug, locale);
        const value = draft[spec.slug]?.[locale] ?? "";
        return (
          <Card key={spec.slug}>
            <CardContent className="space-y-2 p-4">
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-xs text-muted-foreground">{spec.ordinal}</span>
                <h3 className="text-sm font-semibold">{spec.title}</h3>
                <StatusBadge status={status} />
                <span className="ml-auto text-xs text-muted-foreground">{spec.surface}</span>
              </div>
              <Textarea
                value={value}
                onChange={(event) => setDraft({ ...draft, [spec.slug]: { ...draft[spec.slug], [locale]: event.target.value } })}
                className={cn("font-mono text-xs leading-relaxed", spec.size === "document" ? "h-96" : "h-24")}
                placeholder={`Write the ${LOCALE_NAME[locale]} text…`}
              />
              {status === "missing" && (
                <p className="text-xs text-rose-600">
                  Written in {LOCALE_NAME[locale === "en" ? "es" : "en"]} but not here. Publish is refused until it is.
                </p>
              )}
            </CardContent>
          </Card>
        );
      })}

      <PublishWizard open={wizardOpen} onOpenChange={setWizardOpen} draft={draft} />
    </div>
  );
}

const STEPS = ["Completeness", "Preview", "Diff", "Confirm"] as const;

function PublishWizard({ open, onOpenChange, draft }: { open: boolean; onOpenChange: (v: boolean) => void; draft: ArtifactSet }) {
  const [step, setStep] = useState(0);
  const [kind, setKind] = useState<PublishKind>("edition");
  const [reason, setReason] = useState("");
  const [when, setWhen] = useState("");
  const [previewLocale, setPreviewLocale] = useState<Locale>("es");
  const specs = draftSlugs(draft);
  const gaps = completeness(PUBLISHED.policy, draft);
  const changes = changedCells(PUBLISHED.policy, draft);
  const structural = isStructuralChange(PUBLISHED.policy, draft);
  const canAdvance = step !== 0 || gaps.complete;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Publish the Privacy Policy</DialogTitle>
          <DialogDescription>
            Step {step + 1} of {STEPS.length} — {STEPS[step]}
          </DialogDescription>
        </DialogHeader>

        <ol className="flex gap-1 text-xs">
          {STEPS.map((label, index) => (
            <li
              key={label}
              className={cn(
                "flex-1 rounded-sm px-2 py-1 text-center",
                index === step ? "bg-foreground text-background" : index < step ? "bg-muted" : "bg-muted/40 text-muted-foreground",
              )}
            >
              {label}
            </li>
          ))}
        </ol>

        {step === 0 && (
          <div className="space-y-3">
            {gaps.complete ? (
              <Alert>
                <AlertTitle>Complete</AlertTitle>
                <AlertDescription>
                  {specs.length} artifacts × {LOCALES.length} languages, all written.
                </AlertDescription>
              </Alert>
            ) : (
              <Alert>
                <AlertTitle>{gaps.gaps.length} artifact(s) not written</AlertTitle>
                <AlertDescription>
                  <ul className="mt-1 space-y-1 text-xs">
                    {gaps.gaps.map((gap) => (
                      <li key={`${gap.slug}-${gap.locale}`}>
                        {specs.find((s) => s.slug === gap.slug)?.title} — {LOCALE_NAME[gap.locale]}
                      </li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            )}
            {structural && (
              <PrototypeNote>
                This draft adds an artifact. The ordinals shift, so the fingerprint preimage changes shape — a
                correction is refused from step 4.
              </PrototypeNote>
            )}
          </div>
        )}

        {step === 1 && (
          <div className="space-y-3">
            <div className="flex gap-2">
              {LOCALES.map((option) => (
                <Button key={option} size="sm" variant={option === previewLocale ? "default" : "outline"} onClick={() => setPreviewLocale(option)}>
                  {LOCALE_NAME[option]}
                </Button>
              ))}
            </div>
            <PrototypeNote>
              Rendered with the same <code>Markdown</code> component from <code>@ticket-pos/ui</code> that the
              Storefront policy page uses — so this really is what a customer will see.
            </PrototypeNote>
            <div className="space-y-4 rounded-md border bg-background p-6">
              {specs.map((spec) => (
                <section key={spec.slug}>
                  <p className="mb-1 text-xs uppercase tracking-wide text-muted-foreground">{spec.title}</p>
                  <Markdown>{draft[spec.slug]?.[previewLocale] ?? "_Not written._"}</Markdown>
                </section>
              ))}
            </div>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">{changes.length} changed cells against edition 1.</p>
            {changes.map((cell) => (
              <div key={`${cell.slug}-${cell.locale}`} className="rounded-md border p-3">
                <p className="mb-2 flex items-center gap-2 text-sm font-medium">
                  <StatusBadge status={cell.status} />
                  {specs.find((s) => s.slug === cell.slug)?.title} — {LOCALE_NAME[cell.locale]}
                </p>
                <WordDiff
                  before={PUBLISHED.policy[cell.slug]?.[cell.locale] ?? ""}
                  after={draft[cell.slug]?.[cell.locale] ?? ""}
                />
              </div>
            ))}
          </div>
        )}

        {step === 3 && (
          <div className="space-y-4 text-sm">
            <div className="space-y-2">
              <Label>What kind of publish is this?</Label>
              <button
                type="button"
                onClick={() => setKind("edition")}
                className={cn("w-full rounded-md border p-3 text-left", kind === "edition" && "border-foreground bg-accent")}
              >
                <p className="font-medium">New edition — everyone accepts again</p>
                <p className="text-xs text-muted-foreground">
                  {POPULATION.customersHoldingCurrentPolicy.toLocaleString()} customers are asked at next sign-in.
                </p>
              </button>
              <button
                type="button"
                disabled={structural}
                onClick={() => setKind("correction")}
                className={cn(
                  "w-full rounded-md border p-3 text-left disabled:opacity-50",
                  kind === "correction" && "border-foreground bg-accent",
                )}
              >
                <p className="font-medium">Correction — nobody accepts again</p>
                <p className="text-xs text-muted-foreground">
                  {structural
                    ? "Unavailable: this draft adds or removes an artifact."
                    : "For typos and formatting only. Needs a written reason."}
                </p>
              </button>
              <PrototypeNote>
                Two equal-weight cards is exactly the reflex risk #543 asks about. Variant A and C both demote the
                correction instead — worth comparing side by side.
              </PrototypeNote>
            </div>

            {kind === "correction" && (
              <div className="space-y-2">
                <Label htmlFor="wizard-reason">Reason</Label>
                <Textarea id="wizard-reason" value={reason} onChange={(event) => setReason(event.target.value)} className="h-20" />
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="wizard-when">Effective date</Label>
              <Input id="wizard-when" type="date" value={when} onChange={(event) => setWhen(event.target.value)} className="w-44" />
            </div>
          </div>
        )}

        <DialogFooter>
          {step > 0 && (
            <Button variant="outline" onClick={() => setStep(step - 1)}>
              Back
            </Button>
          )}
          {step < STEPS.length - 1 ? (
            <Button disabled={!canAdvance} onClick={() => setStep(step + 1)}>
              {canAdvance ? "Continue" : "Fix the gaps first"}
            </Button>
          ) : (
            <Button disabled={kind === "correction" && reason.trim().length < 12} onClick={() => onOpenChange(false)}>
              {kind === "correction" ? "Publish correction" : `Publish and re-gate ${POPULATION.customersHoldingCurrentPolicy.toLocaleString()}`}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
