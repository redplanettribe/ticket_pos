"use client";

import { useRouter } from "next/navigation";
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
  FormField,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type OperatorInvoiceDetail, issueOperatorInvoiceAgain } from "@/lib/operator-api";

/**
 * Issue again (#580, parent #575, ADR 0068): the one thing a Platform
 * Operator can do to a TERMINALLY DEAD Sale Invoice — owe the Ticket Sale a
 * fresh one.
 *
 * SHOWN ONLY WHEN ALLOWED. The page renders this card on
 * `invoiceLevers(...).issueAgain` alone: a Sale Invoice that is `abandoned`
 * or `annulled`, with no live replacement. Everywhere else the API is
 * certain to refuse, and the card is not there — which is why it is a card
 * of its own rather than a fifth button in the SRI-remedies card, whose
 * levers a dead document never has.
 *
 * THERE IS NO FORM, and the absence is the ruling. The replacement carries
 * the dead document's lines, amounts and RECIPIENT verbatim: reinvoicing
 * must not silently change what was sold or to whom, and a Recipient that
 * needs correcting is the reissue's business (ADR 0061) once the
 * replacement is authorized. #480 left "corrected, or carried over" open;
 * ADR 0068 closes it at carried over, so all this asks for is a note.
 *
 * IT IS A SECOND PRESS, NOT A CONSEQUENCE OF THE FIRST. Abandoning without
 * reissuing stays legal — a Sale the operator does not want reinvoiced —
 * so this is never done for them by the Abandon, and the confirmation says
 * what the press commits the platform to: a fresh document, a fresh number,
 * a mail to the buyer.
 *
 * THE OUTCOME IS A NEW DOCUMENT. The API answers with the replacement — a
 * new id, owed and unsigned, signed by the Drainer on a later round under a
 * freshly allocated secuencial — and the page moves to it; the document it
 * replaced is a click away through the chain card, which reads both ways.
 */

// The note is bounded by the column the reissue's trail is stored in, which
// this act reuses: one chain, one note, one limit.
const NOTE_MAX_LENGTH = 500;

export function OperatorInvoiceIssueAgain({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  function openForm() {
    setNote("");
    setError(null);
    setOpen(true);
  }

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      const replacement = await issueOperatorInvoiceAgain(invoice.id, note.trim() === "" ? null : note.trim());
      setOpen(false);
      toast.success(t("invoicingIssueAgainDone"));
      router.push(`/operator/invoicing/${replacement.id}`);
    } catch (submitError) {
      setError(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("invoicingIssueAgainFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingIssueAgainCardTitle")}</CardTitle>
        <CardDescription>{t("invoicingIssueAgainCardDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <Button type="button" disabled={busy} onClick={openForm}>
          {busy ? t("invoicingIssuingAgain") : t("invoicingIssueAgain")}
        </Button>
        {/*
          What the press does that the button cannot say: a fresh number, the
          dead one kept and never reused, and the buyer mailed the
          replacement alone.
        */}
        <p className="text-xs text-muted-foreground">{t("invoicingIssueAgainHint")}</p>
      </CardContent>

      <Dialog open={open} onOpenChange={(next) => (busy ? null : setOpen(next))}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("invoicingIssueAgainConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("invoicingIssueAgainConfirm")}</DialogDescription>
          </DialogHeader>
          {/*
            The note travels with the chain and is read beside every document
            the replacement concerns — the operator's own words about why a
            second document exists for one Sale.
          */}
          <FormField
            id="issue_again_note"
            label={t("invoicingIssueAgainNoteLabel")}
            description={t("invoicingIssueAgainNotePlaceholder")}
          >
            <Textarea
              value={note}
              disabled={busy}
              maxLength={NOTE_MAX_LENGTH}
              rows={3}
              onChange={(event) => setNote(event.target.value)}
            />
          </FormField>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => setOpen(false)}>
              {t("invoicingIssueAgainCancel")}
            </Button>
            <Button type="button" disabled={busy} onClick={() => void submit()}>
              {busy ? t("invoicingIssuingAgain") : t("invoicingIssueAgain")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
