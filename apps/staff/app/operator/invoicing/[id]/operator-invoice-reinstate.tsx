"use client";

import { useState } from "react";

import { Alert, AlertDescription, AlertTitle, Button, FormField, Input, Textarea, toast } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { reinstateReady } from "@/lib/annulment-panel";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type OperatorInvoiceDetail, reinstateOperatorInvoice } from "@/lib/operator-api";

// The note is bounded exactly as Mark annulled's is.
const NOTE_MAX_LENGTH = 500;

/**
 * Reinstate's confirm (#749, spec #742, ADR 0078 section 5): the factura's
 * number typed in full and an optional note, with what the act owes and what
 * the buyer hears said before the press. Drawn on the annulled face of the
 * annulment panel and in the needs-attention entry of a factura at mismatch,
 * wherever the API admits it; the API decides, and refuses a press that
 * raced a moving check with the code it answers.
 */
export function OperatorInvoiceReinstate({
  invoiceId,
  number,
  onReinstated,
}: {
  invoiceId: string;
  number: string;
  onReinstated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const [typedNumber, setTypedNumber] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ready = reinstateReady({ number, typedNumber });

  async function reinstate() {
    setBusy(true);
    setError(null);
    try {
      const invoice = await reinstateOperatorInvoice(invoiceId, {
        number: typedNumber.trim(),
        note: note.trim() === "" ? null : note.trim(),
      });
      toast.success(t("invoicingReinstateDone"));
      onReinstated(invoice);
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t("invoicingReinstateFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3 rounded-lg bg-muted/40 p-4 text-left">
      <p className="text-sm font-medium text-foreground">{t("invoicingReinstateTitle")}</p>
      <p className="text-xs text-muted-foreground">{t("invoicingReinstateHint")}</p>
      <p className="text-xs text-muted-foreground">{t("invoicingReinstateOwes")}</p>
      <p className="text-xs text-muted-foreground">{t("invoicingReinstateBuyer")}</p>
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <FormField id={`reinstate_number_${invoiceId}`} label={t("invoicingReinstateNumberLabel", { number })}>
        <Input
          value={typedNumber}
          placeholder={number}
          autoComplete="off"
          className="font-mono"
          onChange={(event) => setTypedNumber(event.target.value)}
        />
      </FormField>
      <FormField id={`reinstate_note_${invoiceId}`} label={t("invoicingReinstateNoteLabel")}>
        <Textarea
          value={note}
          maxLength={NOTE_MAX_LENGTH}
          rows={2}
          placeholder={t("invoicingReinstateNotePlaceholder")}
          onChange={(event) => setNote(event.target.value)}
        />
      </FormField>
      <Button type="button" className="w-full" disabled={!ready || busy} onClick={() => void reinstate()}>
        {busy ? t("invoicingReinstateSubmitting") : t("invoicingReinstateSubmit")}
      </Button>
    </div>
  );
}
