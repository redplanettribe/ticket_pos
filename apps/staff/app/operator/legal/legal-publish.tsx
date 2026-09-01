"use client";

import { useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
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
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { canPublishDraft, correctionBlocker } from "@/lib/legal-publish";
import {
  publishOperatorLegalEdition,
  type OperatorLegalDocument,
  type OperatorLegalWorkspace,
} from "@/lib/operator-api";

/**
 * The publish step (#563, spec #556): the one act in the Legal Center a reader
 * can see.
 *
 * THE RAIL IS THE WHOLE DESIGN OF THIS COMPONENT, and it is worth stating before
 * anything else on the screen is explained. There are two acts. A NEW EDITION
 * re-gates everybody — it asks the entire customer base to accept again — and a
 * CORRECTION re-gates nobody. The re-gating one is the SAFE one: it is what you
 * want if you are not sure, because the worst it costs is a checkbox somebody
 * ticks again, while the worst a wrongly-chosen correction costs is a changed
 * contract nobody was ever asked about.
 *
 * So the new edition is the big, obvious, default button, and the correction is
 * a QUIET LINK BENEATH IT — never a second button of equal weight, never a pair
 * of radio buttons, never a dropdown. "Re-gate nobody" must not be a coin toss
 * that an operator in a hurry can lose by clicking the wrong half of a pair.
 *
 * THE BUTTON NAMES THE LABEL IT WOULD CREATE, so choosing the quiet link
 * visibly turns `2` into `1.2`, and THE CONFIRM BUTTON CARRIES THE HEADCOUNT, so
 * the consequence is displayed before it is accepted. That display is not a
 * courtesy: production holds ONE Platform Operator, so there is nobody to
 * approve anything, and proving the operator was SHOWN the consequence is the
 * substitute for a second pair of eyes. The same figure is stored on the version
 * row, so what was on screen is what the record says was on screen.
 *
 * NOTHING HERE IS A GUARANTEE. Every rule below is enforced at the HTTP seam,
 * because a precondition a browser could decline to check is not a precondition.
 * What this component buys is that an operator meets a refusal while they can
 * still act on it.
 */

type LegalPublishProps = {
  document: OperatorLegalDocument;
  workspace: OperatorLegalWorkspace;
  /** True while there are unsaved changes: what would publish is the SAVED draft. */
  dirty: boolean;
  /** Adopts the workspace the publication answered with. */
  onPublished: (workspace: OperatorLegalWorkspace) => void;
};

export function LegalPublish({ document, workspace, dirty, onPublished }: LegalPublishProps) {
  const t = useTranslations("operator.legal");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const readerLocale = useLocale();

  const plan = workspace.publish;
  const [effectiveDate, setEffectiveDate] = useState("");
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState<"edition" | "correction" | null>(null);
  const [publishing, setPublishing] = useState(false);

  // The three gates, from the SAVED draft's own facts. Not from the textareas:
  // what would publish is what was saved, previewed and diffed.
  const canPublish =
    !dirty &&
    canPublishDraft({
      complete: plan.complete,
      previewedAll: workspace.draft.previewed_all,
      seenDiff: workspace.draft.seen_diff,
    });
  const structural = plan.structural;
  const blocker = correctionBlocker({
    canPublish,
    structural,
    localeSetChanged: plan.locale_set_changed,
    emptyDiff: plan.empty_diff,
  });

  const headcount = new Intl.NumberFormat(readerLocale).format(plan.headcount);

  const correctionTitle = () => {
    switch (blocker) {
      case "structural":
        // Ruled copy: the one structural change the code can prove is not a typo.
        return t("correctionRefusedStructural");
      case "locales":
        return t("correctionRefusedLocales");
      case "empty":
        return t("correctionRefusedEmpty");
      case "gates":
        return t("publishBlocked");
      default:
        return undefined;
    }
  };

  const publish = async (kind: "edition" | "correction") => {
    setPublishing(true);
    try {
      const next = await publishOperatorLegalEdition(document, {
        kind,
        // A correction NAMES NO DATE: it takes effect immediately, and the API
        // refuses one rather than quietly dropping it.
        ...(kind === "edition" ? { effective_date: effectiveDate } : {}),
        ...(kind === "correction" ? { reason } : {}),
      });
      onPublished(next);
      setConfirming(null);
      setEffectiveDate("");
      setReason("");
      toast.success(kind === "edition" ? t("published") : t("publishedCorrection"));
    } catch (error: unknown) {
      toast.error(error instanceof ApiError ? apiErrorMessage(errorCopy, error) : t("publishFailed"));
    } finally {
      setPublishing(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("publishHeading")}</CardTitle>
        <CardDescription>{t("publishHint")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/*
          THE PROTECTED LANGUAGE. Neither publication may drop it, and the reason
          is one of two sentences resting on two different footings — a statute
          for the notice, a clause of the contract for the agreement. Shown here
          rather than only at the refusal, because an operator who has unticked
          Spanish should learn why before they reach a button.
        */}
        {!plan.protected_locale_kept ? (
          <Alert variant="destructive">
            <AlertTitle>{t("protectedLocaleTitle")}</AlertTitle>
            <AlertDescription>
              {document === "policy" ? t("protectedLocalePolicy") : t("protectedLocaleTerms")}
            </AlertDescription>
          </Alert>
        ) : null}

        {!canPublish ? <p className="text-sm text-muted-foreground">{t("publishBlocked")}</p> : null}

        {/*
          THE EFFECTIVE DATE LIVES HERE AND NOT IN THE EDITOR: when the text takes
          effect is a property of the ACT, not of the words. Date-only, 00:00 in
          Ecuador, and no earlier than tomorrow — an irreversible re-gate gets a
          night in which the operator can change their mind. The correction has no
          date box at all, because it is immediate.
        */}
        <div className="space-y-2">
          <Label htmlFor="publish-effective-date">{t("effectiveDateLabel")}</Label>
          <Input
            id="publish-effective-date"
            type="date"
            min={plan.earliest_effective_date}
            value={effectiveDate}
            onChange={(event) => setEffectiveDate(event.target.value)}
            className="max-w-xs"
          />
          <p className="text-xs text-muted-foreground">
            {t("effectiveDateHint", { date: plan.earliest_effective_date })}
          </p>
        </div>

        <div className="space-y-2">
          {/* The default act, named after the label it would create. */}
          <Button
            type="button"
            size="lg"
            disabled={!canPublish || !effectiveDate}
            onClick={() => setConfirming("edition")}
          >
            {t("publishEdition", { label: plan.gating_label })}
          </Button>
          <p className="text-sm text-muted-foreground">
            {t("publishEditionConsequence", { n: headcount })}
          </p>

          {/*
            THE QUIET LINK. Smaller, plainer, beneath — never an equal option.
            It names the label the correction would take, so choosing it visibly
            turns the edition's number into a revision of the current one.
          */}
          <div className="pt-2">
            <Button
              type="button"
              variant="link"
              size="sm"
              className="h-auto p-0 text-muted-foreground"
              disabled={!canPublish || structural}
              title={correctionTitle()}
              onClick={() => setConfirming("correction")}
            >
              {t("publishCorrectionLink", { label: plan.correction_label })}
            </Button>
            {blocker && blocker !== "gates" ? (
              <p className="text-xs text-muted-foreground">{correctionTitle()}</p>
            ) : null}
          </div>
        </div>
      </CardContent>

      {/* The new edition's confirmation: the headcount is the whole of it. */}
      <Dialog open={confirming === "edition"} onOpenChange={(open) => !open && setConfirming(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("publishConfirmTitle", { label: plan.gating_label })}</DialogTitle>
            <DialogDescription>
              {t("publishConfirmBody", { n: headcount, date: effectiveDate })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirming(null)}>
              {tOperator("cancel")}
            </Button>
            <Button type="button" disabled={publishing} onClick={() => void publish("edition")}>
              {publishing ? t("publishing") : t("publishConfirmAction", { n: headcount })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* The correction's: a typed reason, and a promise that nobody is asked again. */}
      <Dialog open={confirming === "correction"} onOpenChange={(open) => !open && setConfirming(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("correctionConfirmTitle", { label: plan.correction_label })}</DialogTitle>
            <DialogDescription>{t("correctionConfirmBody")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="correction-reason">{t("correctionReasonLabel")}</Label>
            {/*
              THE ONE THING THE BYTES CANNOT SAY. The diff records what moved; this
              records what it was for, and a correction is the act with no other
              justification on the record — a new edition's justification is its
              own text.
            */}
            <Textarea
              id="correction-reason"
              rows={3}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t("correctionReasonHint")}</p>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirming(null)}>
              {tOperator("cancel")}
            </Button>
            <Button
              type="button"
              disabled={publishing || reason.trim().length < 10}
              onClick={() => void publish("correction")}
            >
              {publishing ? t("publishing") : t("correctionConfirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
