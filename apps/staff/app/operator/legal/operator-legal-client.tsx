"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  PageHeader,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import {
  cellStatus,
  completeness,
  draftSlugs,
  hasChanges,
  isStructuralChange,
  toArtifactSet,
  type ArtifactSet,
  type CellStatus,
} from "@/lib/legal-drafts";
import {
  discardOperatorLegalDraft,
  fetchOperatorLegalWorkspace,
  saveOperatorLegalDraft,
  type OperatorLegalDocument,
  type OperatorLegalWorkspace,
} from "@/lib/operator-api";

/**
 * The Legal Center (#561, spec #556): where the platform's own agreements — the
 * Privacy Policy and the Términos y Condiciones — are written.
 *
 * ONE MUTABLE DRAFT PER DOCUMENT, HELD BY THE API. Every keystroke here is
 * local until Save, and Save replaces the whole draft server-side; a reload,
 * another laptop or a closed tab all reopen on the same draft. Discard throws it
 * away and reopens on the current published edition, so an experiment is not a
 * commitment.
 *
 * EVERY EDITABLE CELL IS A PLAIN MONOSPACE BOX AND NEVER A RICH-TEXT EDITOR, and
 * this is the single most load-bearing decision on the screen. The renderer
 * these artifacts are read through (packages/ui markdown.tsx) runs
 * `remark-breaks` and does NOT include `rehype-raw`: a hard-wrapped line becomes
 * a <br> in the published document, and raw HTML is dropped without a word. The
 * artifacts are authored one line per block today, and anything that reflowed
 * text or emitted markup would silently mangle a document that people are held
 * to. A monospace box makes the line structure visible, which is the only
 * defence available.
 *
 * ONE-LINE LABELS AND FULL DOCUMENTS GET DIFFERENT AFFORDANCES — a checkbox
 * label does not get a full-page editor — and each artifact says where it
 * appears in the wild, so nobody edits the Short Notice thinking it is the
 * policy.
 *
 * NOTHING HERE PUBLISHES. The preview and diff are #562, the publication is
 * #563. The completeness dots and the structural-change note are shown now
 * because they are the rules that will refuse a publication then, and an
 * operator should learn about a hole while they can still fill it.
 */

type DraftRow = { slug: string; bodies: Partial<Record<AppLocale, string>> };

const DOCUMENTS: OperatorLegalDocument[] = ["policy", "terms"];

/** A slug an operator invents must look like the ones the platform already uses. */
const SLUG_PATTERN = /^[a-z][a-z0-9-]*$/;

export function OperatorLegalClient() {
  const t = useTranslations("operator.legal");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const readerLocale = toAppLocale(useLocale());

  const [document, setDocument] = useState<OperatorLegalDocument>("policy");
  const [workspace, setWorkspace] = useState<OperatorLegalWorkspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [rows, setRows] = useState<DraftRow[]>([]);
  const [publishedLocales, setPublishedLocales] = useState<AppLocale[]>([]);
  const [dirty, setDirty] = useState(false);

  const [focused, setFocused] = useState<AppLocale | null>(null);
  const [newSlug, setNewSlug] = useState("");
  const [saving, setSaving] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  /**
   * The words for a language and for a cell's state, resolved STATICALLY.
   * next-intl's keys are typed from en.json (global.d.ts), so a key assembled
   * from a SLUG — which an operator invents — could not be typed at all. These
   * two are assembled from closed unions, and the inventory below is a plain
   * table of static calls.
   */
  const localeName = (locale: AppLocale) => (locale === "en" ? t("localeEn") : t("localeEs"));
  const statusLabel = (status: CellStatus) => {
    switch (status) {
      case "unchanged":
        return t("statusUnchanged");
      case "modified":
        return t("statusModified");
      case "added":
        return t("statusAdded");
      case "removed":
        return t("statusRemoved");
      default:
        return t("statusMissing");
    }
  };

  /**
   * Where each artifact appears in the wild (#558's inventory), so nobody edits
   * a checkbox label thinking it is the policy. A slug that is not in here is
   * one the operator invented and nothing renders — which the editor says out
   * loud, because publishing text no surface asks for is worth noticing before
   * rather than after.
   */
  const surfaces: Record<string, string> = {
    "short-notice": t("surfaceShortNotice"),
    "label-policy-acceptance": t("surfacePolicyAcceptance"),
    "label-marketing-consent": t("surfaceMarketingConsent"),
    "label-networking-consent": t("surfaceNetworkingConsent"),
    policy: t("surfacePolicy"),
    "label-terms-acceptance": t("surfaceTermsAcceptance"),
    terms: t("surfaceTerms"),
  };

  /** Adopts a workspace as the screen's whole truth: what was read, or what a write answered with. */
  const adopt = useCallback((next: OperatorLegalWorkspace) => {
    setWorkspace(next);
    setRows(next.draft.artifacts.map((artifact) => ({ slug: artifact.slug, bodies: { ...artifact.bodies } })));
    setPublishedLocales([...next.draft.published_locales]);
    setDirty(false);
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    fetchOperatorLegalWorkspace(document)
      .then((next) => {
        if (cancelled) return;
        adopt(next);
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setLoadError(error instanceof ApiError ? apiErrorMessage(errorCopy, error) : t("loadFailed"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // errorCopy and t are stable for a render tree; the read is keyed on the document.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [document, adopt]);

  const supportedLocales = workspace?.supported_locales ?? [];
  const columns = focused ? [focused] : supportedLocales;

  const publishedSet: ArtifactSet = useMemo(
    () => toArtifactSet(workspace?.published.artifacts ?? []),
    [workspace],
  );
  const draftSet: ArtifactSet = useMemo(
    () => toArtifactSet(rows.map((row, index) => ({ slug: row.slug, ordinal: index + 1, bodies: row.bodies }))),
    [rows],
  );

  const specs = useMemo(() => draftSlugs(draftSet), [draftSet]);
  const gaps = useMemo(
    () => completeness(publishedSet, draftSet, publishedLocales),
    [publishedSet, draftSet, publishedLocales],
  );
  const structural = useMemo(
    () => isStructuralChange(publishedSet, draftSet, publishedLocales),
    [publishedSet, draftSet, publishedLocales],
  );
  const changed = useMemo(
    () => hasChanges(publishedSet, draftSet, publishedLocales),
    [publishedSet, draftSet, publishedLocales],
  );

  const setBody = (slug: string, locale: AppLocale, body: string) => {
    setRows((current) =>
      current.map((row) => (row.slug === slug ? { ...row, bodies: { ...row.bodies, [locale]: body } } : row)),
    );
    setDirty(true);
  };

  const addArtifact = () => {
    const slug = newSlug.trim().toLowerCase();
    if (!SLUG_PATTERN.test(slug) || rows.some((row) => row.slug === slug)) {
      toast.error(t("addRefused"));
      return;
    }
    // Appended, so it takes the last ordinal. An operator who wants it earlier
    // moves it, and moving renumbers — the ordinal is the fingerprint preimage's
    // order, so where an artifact sits is a real decision about the document.
    setRows((current) => [...current, { slug, bodies: {} }]);
    setNewSlug("");
    setDirty(true);
  };

  const removeArtifact = (slug: string) => {
    setRows((current) => current.filter((row) => row.slug !== slug));
    setDirty(true);
  };

  const moveArtifact = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= rows.length) return;
    setRows((current) => {
      const next = [...current];
      const [moved] = next.splice(index, 1);
      next.splice(target, 0, moved);
      return next;
    });
    setDirty(true);
  };

  const toggleLocale = (locale: AppLocale) => {
    setPublishedLocales((current) =>
      current.includes(locale) ? current.filter((entry) => entry !== locale) : [...current, locale],
    );
    setDirty(true);
  };

  const save = async () => {
    setSaving(true);
    try {
      const next = await saveOperatorLegalDraft(document, {
        published_locales: publishedLocales,
        artifacts: rows.map((row) => {
          const bodies: Record<string, string> = {};
          for (const [locale, body] of Object.entries(row.bodies)) {
            if (typeof body === "string") bodies[locale] = body;
          }
          return { slug: row.slug, bodies };
        }),
      });
      adopt(next);
      toast.success(t("saved"));
    } catch (error: unknown) {
      toast.error(error instanceof ApiError ? apiErrorMessage(errorCopy, error) : t("saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const discard = async () => {
    setDiscarding(true);
    try {
      const next = await discardOperatorLegalDraft(document);
      adopt(next);
      toast.success(t("discarded"));
    } catch (error: unknown) {
      toast.error(error instanceof ApiError ? apiErrorMessage(errorCopy, error) : t("discardFailed"));
    } finally {
      setDiscarding(false);
      setConfirmDiscard(false);
    }
  };

  if (loading) {
    return <p className="text-muted-foreground">{t("loading")}</p>;
  }

  if (loadError || !workspace) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
        <AlertDescription>{loadError ?? t("loadFailed")}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("title")} description={t("description")} />

      {/* One tab per document. They version independently (ADR 0066) and are never edited together. */}
      <div className="flex gap-2">
        {DOCUMENTS.map((kind) => (
          <Button
            key={kind}
            type="button"
            variant={kind === document ? "default" : "outline"}
            onClick={() => setDocument(kind)}
          >
            {kind === "policy" ? t("documentPolicy") : t("documentTerms")}
          </Button>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t("publishedHeading")}</CardTitle>
          <CardDescription>
            {t("publishedSummary", {
              label: workspace.published.label,
              date: workspace.published.effective_date,
            })}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="font-mono text-xs text-muted-foreground break-all">
            {workspace.published.content_hash}
          </p>
          <p className="text-sm text-muted-foreground">
            {workspace.draft.stored && workspace.draft.updated_at
              ? t("draftSavedBy", {
                  who: workspace.draft.updated_by,
                  when: formatDateTime(workspace.draft.updated_at, PLATFORM_TIME_ZONE, readerLocale) ?? workspace.draft.updated_at,
                })
              : t("draftNeverSaved")}
          </p>
          {dirty ? (
            <p className="text-sm font-medium">{t("unsavedChanges")}</p>
          ) : null}
          {!workspace.draft.base_is_current ? (
            // Somebody published underneath this draft. A warning and not a
            // refusal: the work is still the operator's, and only publishing has
            // to decide what to do about it (#563).
            <Alert>
              <AlertTitle>{t("baseStaleTitle")}</AlertTitle>
              <AlertDescription>{t("baseStale")}</AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("languagesHeading")}</CardTitle>
          <CardDescription>{t("languagesHint")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/*
            The published-language set is EXPLICIT and bounded by the platform's
            app locales, which the API sends: a half-translated language must be
            a draft that cannot publish, not a language quietly dropped by an
            empty textarea. Both documents publish both languages today, so
            dropping is the only direction there is to exercise.
          */}
          <div className="flex flex-wrap gap-4">
            {supportedLocales.map((locale) => (
              <label key={locale} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={publishedLocales.includes(locale)}
                  onChange={() => toggleLocale(locale)}
                />
                <span>{localeName(locale)}</span>
              </label>
            ))}
          </div>
          {publishedLocales.length === 0 ? (
            <p className="text-sm text-destructive">{t("noLanguages")}</p>
          ) : null}

          {/* Focus one: a ~280-line document is unreadable in half a screen. */}
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm text-muted-foreground">{t("focusHeading")}</span>
            <Button
              type="button"
              size="sm"
              variant={focused === null ? "default" : "outline"}
              onClick={() => setFocused(null)}
            >
              {t("focusBoth")}
            </Button>
            {supportedLocales.map((locale) => (
              <Button
                key={locale}
                type="button"
                size="sm"
                variant={focused === locale ? "default" : "outline"}
                onClick={() => setFocused(locale)}
              >
                {localeName(locale)}
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>

      <Alert>
        <AlertTitle>{t("plainTextTitle")}</AlertTitle>
        <AlertDescription>{t("plainText")}</AlertDescription>
      </Alert>

      {specs.map((spec, index) => (
        <Card key={spec.slug}>
          <CardHeader>
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div>
                <CardTitle className="font-mono text-base">{spec.slug}</CardTitle>
                <CardDescription>
                  {/* Where this text appears in the wild (#558's inventory), so
                      nobody edits a checkbox label thinking it is the policy. */}
                  {spec.known ? surfaces[spec.slug] : t("surfaceUnknown")}
                </CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="outline">{t("ordinal", { n: spec.ordinal })}</Badge>
                <Badge variant="outline">{spec.size === "line" ? t("sizeLine") : t("sizeDocument")}</Badge>
                <Button type="button" size="sm" variant="outline" onClick={() => moveArtifact(index, -1)}>
                  {t("moveUp")}
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={() => moveArtifact(index, 1)}>
                  {t("moveDown")}
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={() => removeArtifact(spec.slug)}>
                  {t("removeArtifact")}
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {/* Parallel columns (#543 chose this wholesale), collapsing to one under Focus one. */}
            <div className={columns.length > 1 ? "grid gap-4 lg:grid-cols-2" : "grid gap-4"}>
              {columns.map((locale) => {
                const status = cellStatus(publishedSet, draftSet, spec.slug, locale);
                const publishesThis = publishedLocales.includes(locale);
                return (
                  <div key={locale} className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <Label htmlFor={`${spec.slug}-${locale}`} className="flex items-center gap-2">
                        <CompletenessDot status={status} label={statusLabel(status)} />
                        <span>{localeName(locale)}</span>
                      </Label>
                      {publishesThis ? null : (
                        <span className="text-xs text-muted-foreground">{t("notPublishedHere")}</span>
                      )}
                    </div>
                    <Textarea
                      id={`${spec.slug}-${locale}`}
                      // MONOSPACE, and the line structure left alone: a hard
                      // wrap here is a <br> on the published page.
                      className="font-mono text-sm"
                      spellCheck={false}
                      rows={spec.size === "line" ? 2 : 20}
                      value={rows[index]?.bodies[locale] ?? ""}
                      onChange={(event) => setBody(spec.slug, locale, event.target.value)}
                    />
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      ))}

      <Card>
        <CardHeader>
          <CardTitle>{t("addHeading")}</CardTitle>
          <CardDescription>{t("addHint")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-end gap-2">
          <div className="space-y-2">
            <Label htmlFor="new-artifact-slug">{t("addSlugLabel")}</Label>
            <Input
              id="new-artifact-slug"
              className="font-mono"
              value={newSlug}
              onChange={(event) => setNewSlug(event.target.value)}
              placeholder="label-analytics-consent"
            />
          </div>
          <Button type="button" variant="outline" onClick={addArtifact}>
            {t("addArtifact")}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4 pt-6">
          <p className="text-sm">
            {gaps.complete ? t("complete") : t("incomplete", { n: gaps.gaps.length })}
          </p>
          {structural ? <p className="text-sm">{t("structural")}</p> : null}
          {!changed && !dirty ? <p className="text-sm text-muted-foreground">{t("sameAsPublished")}</p> : null}
          <div className="flex flex-wrap gap-2">
            <Button type="button" onClick={save} disabled={saving || publishedLocales.length === 0}>
              {saving ? t("saving") : t("save")}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmDiscard(true)}
              disabled={discarding}
            >
              {t("discard")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Dialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("discardConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("discardConfirm")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmDiscard(false)}>
              {tOperator("cancel")}
            </Button>
            <Button type="button" onClick={discard} disabled={discarding}>
              {discarding ? t("discarding") : t("discardConfirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * The completeness dot: one per artifact per language, so an operator can see at
 * a glance which cells are still empty. The colour is never the whole message —
 * the status is on the label too — because a dot alone is not readable to
 * everybody.
 */
function CompletenessDot({ status, label }: { status: CellStatus; label: string }) {
  const tone =
    status === "missing"
      ? "bg-destructive"
      : status === "unchanged"
        ? "bg-muted-foreground"
        : "bg-primary";
  return (
    <span className="inline-flex items-center gap-1">
      <span aria-hidden className={`inline-block size-2 rounded-full ${tone}`} />
      <span className="sr-only">{label}</span>
    </span>
  );
}
