"use client";

import { useState } from "react";

import { Alert, AlertDescription, AlertTitle, Button, toast } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import {
  cancelOperatorLegalEdition,
  type OperatorLegalDocument,
  type OperatorLegalWorkspace,
} from "@/lib/operator-api";

/**
 * The scheduled-edition banner (#564, spec #556): what is about to happen, and
 * the way to stop it.
 *
 * A gating edition cannot take effect the day it is published — an irreversible
 * re-gate gets a night in which the operator can change their mind — so between
 * the click and the midnight rollover there is a night. This component is what
 * makes that night usable, and it is arranged around two decisions.
 *
 * FIRST, IT IS PERSISTENT AND IT IS AT THE TOP. Not a toast that a publication
 * shows once and not a line in a history table somebody would have to go
 * looking for: an edition that will re-gate the entire customer base tomorrow
 * morning is the most important fact on this screen for as long as it is true,
 * and the operator who has to remember it is the same one who is about to spend
 * an hour editing the next draft underneath it.
 *
 * SECOND, THE CANCEL BUTTON HAS NO CEREMONY — no confirmation dialog, no typed
 * reason, no "are you sure". The publish button has all three, and deliberately;
 * this one is its mirror. UNDOING IS ALWAYS CHEAPER THAN DOING: the act being
 * withdrawn re-gates every Customer and everybody on the staff platform, and the
 * withdrawal, while the edition is on nobody's screen, moves not one person.
 * Pricing the two alike is how somebody ends up publishing an edition they had
 * changed their mind about, because taking it back looked expensive.
 *
 * THE CONTROL'S EXISTENCE IS MEMBERSHIP OF `scheduled`, and there is no date
 * arithmetic here at all. Whether an edition's day has come is the database's
 * own answer about its own day; a browser comparing the effective date against
 * the reader's clock would offer a button in one time zone that the seam refuses
 * in another. An edition leaves the list at midnight, and the API refuses the
 * act at the same instant.
 */

type LegalScheduledProps = {
  document: OperatorLegalDocument;
  workspace: OperatorLegalWorkspace;
  /** Adopts the workspace the cancellation answered with. */
  onCancelled: (workspace: OperatorLegalWorkspace) => void;
};

export function LegalScheduled({ document, workspace, onCancelled }: LegalScheduledProps) {
  const t = useTranslations("operator.legal");
  const errorCopy = useMessages().errors;
  const [cancelling, setCancelling] = useState<string | null>(null);

  // Empty is the normal state, and it renders nothing rather than an empty card
  // saying so: a reminder about nothing is noise, and noise is how a reminder
  // about something stops being read.
  if (workspace.scheduled.length === 0) {
    return null;
  }

  const cancel = async (versionID: string, label: string) => {
    setCancelling(versionID);
    try {
      onCancelled(await cancelOperatorLegalEdition(document, versionID));
      toast.success(t("scheduledCancelled", { label }));
    } catch (error: unknown) {
      // Including the one refusal a page left open overnight will meet: the day
      // came while the button was still on screen. The API's sentence is shown,
      // because it is the one that knows which day passed.
      toast.error(error instanceof ApiError ? apiErrorMessage(errorCopy, error) : t("scheduledCancelFailed"));
    } finally {
      setCancelling(null);
    }
  };

  return (
    <div className="space-y-3">
      {workspace.scheduled.map((edition) => (
        <Alert key={edition.version_id}>
          <AlertTitle>
            {edition.gating
              ? t("scheduledGatingTitle", { label: edition.label, date: edition.effective_date })
              : t("scheduledCorrectionTitle", { label: edition.label, date: edition.effective_date })}
          </AlertTitle>
          <AlertDescription className="space-y-3">
            <p>{edition.gating ? t("scheduledGating") : t("scheduledCorrection")}</p>
            {/*
              The button says what it does and nothing follows it. `variant`
              "outline" and not "destructive": cancelling is the SAFE act here,
              and dressing it in the colour of danger would be the screen telling
              an operator to think twice about the cheap half of the pair.
            */}
            <div className="flex flex-wrap items-center gap-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={cancelling !== null}
                onClick={() => cancel(edition.version_id, edition.label)}
              >
                {cancelling === edition.version_id ? t("scheduledCancelling") : t("scheduledCancel")}
              </Button>
              <span className="text-xs text-muted-foreground">{t("scheduledCancelHint")}</span>
            </div>
          </AlertDescription>
        </Alert>
      ))}
    </div>
  );
}
