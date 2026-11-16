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
  FormField,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { hasInvoiceLevers, invoiceLevers, pressToast, resendHintAskedAt } from "@/lib/invoice-actions";
import {
  type OperatorInvoiceDetail,
  abandonOperatorInvoice,
  annulOperatorInvoice,
  checkOperatorInvoice,
  resendOperatorInvoice,
} from "@/lib/operator-api";

/**
 * Check status and Resend (#455): the two things a Platform Operator can do
 * to a Tax Invoice the SRI has not authorized.
 *
 * SHOWN ONLY WHEN ALLOWED. An authorized invoice is a legal artifact and the
 * API refuses both actions on it, so this card is not rendered for one at all
 * rather than rendered disabled: there is nothing to do, and a greyed button
 * would invite the question of why.
 *
 * THE OUTCOME IS THE INVOICE. Both calls answer with the invoice as it then
 * stands and hand it to the page, which re-renders status, messages,
 * authorization and the ledger from it. The toast says what the SRI answered
 * the press - the attempt the API names - never the status (#736, see
 * `pressToast`).
 * WHO HOLDS THE DOCUMENT is what the two hints say, and they are never both
 * true (#516). When the SRI holds it and is still deciding — after a plain
 * RECIBIDA, or after a resend the SRI met with "clave already registered"
 * (43) or "in processing" (70) — the API says so with `check_status_hint`,
 * and the card shows "it is there, check status" instead of an error. When
 * no submit was ever acknowledged and the SRI's last answer was that it
 * holds nothing under the clave - where a submit that died in transport
 * leaves it - `resend_hint` says so instead, and the card reports that
 * answer and when it was given (#728), and asks for the resend. It never
 * says the document was not received: the SRI's status services give that
 * answer for documents they hold (#722), and resending is the fix either
 * way, since a clave it holds is answered as already registered. A
 * document the ledger says neither about shows neither banner: the card
 * offers its levers and says nothing it cannot know.
 *
 * RESEND IS REFUSED WHERE THE NUMBER IS THE OBJECTION (#577, ADR 0068).
 * On a document the SRI answered 45 "secuencial registrado", a resend would
 * carry the same secuencial the SRI already refuses and can only earn the
 * same answer — the API refuses it, so the button is not offered. Unlike
 * every other missing lever here, that absence does not follow from the
 * status, so this card says why in its own sentence rather than leaving a
 * shorter row of buttons to be puzzled over. Check status is untouched: it
 * asks and never sends.
 *
 * MARK ANNULLED (#477) is the third lever, offered only on a pending or
 * needs_attention document — where the operator may have annulled it by
 * hand at the SRI portal, which the SRI offers no web service for. It is a
 * record, not a request: nothing goes to the SRI, and it is irreversible,
 * so it is confirmed first.
 *
 * ABANDON TAKES ITS PLACE WHERE THE NUMBER IS REFUSED (#578, ADR 0068). On
 * such a document there is nothing at the portal to have been annulled, so
 * Mark annulled would record an act that never happened and the API refuses
 * it; Abandon records the true one — the SRI never took this document and
 * never will. The two are never offered together, and this card says why the
 * one the operator was reaching for is gone, in its own sentence, for the
 * same reason it does so for Resend.
 *
 * ABANDON RESTS ON A FRESH CHECK, TWICE, which the page does not judge: the
 * API wants the last two things on the ledger to be checks the SRI answered
 * "nothing known", at least a minute apart, the later within the last
 * quarter hour (ADR 0068, amended for #722, because one such answer is not
 * proof), and answers INVOICE_CHECK_NOT_FRESH otherwise. That refusal has a
 * next step rather than a dead end - Check status twice, at least a minute
 * apart, then Abandon - so the button is offered and the refusal teaches,
 * rather than the button being hidden on a rule the operator would have to
 * guess at.
 *
 * Which levers show is `invoiceLevers`' decision.
 * Reissue (#483) is that decision's fourth lever and its own card
 * (operator-invoice-reissue.tsx): it belongs to an authorized document,
 * which this card — the SRI's remedies — never renders for.
 */

// The abandonment note is bounded exactly as the reissue's is, by the same
// schema column: a sentence for a colleague, not a file.
const NOTE_MAX_LENGTH = 500;

export function OperatorInvoiceActions({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const errorCopy = useMessages().errors;
  const [busy, setBusy] = useState<"check" | "resend" | "annul" | "abandon" | null>(null);
  const [confirmingAnnulment, setConfirmingAnnulment] = useState(false);
  const [confirmingAbandonment, setConfirmingAbandonment] = useState(false);
  const [abandonNote, setAbandonNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  const levers = invoiceLevers(invoice.status, invoice.ecuador !== null, undefined, invoice.refused_by_number);
  const resendAskedAt = resendHintAskedAt(invoice);
  if (!hasInvoiceLevers(levers)) {
    return null;
  }

  async function run(action: "check" | "resend") {
    setBusy(action);
    setError(null);
    try {
      const updated =
        action === "check" ? await checkOperatorInvoice(invoice.id) : await resendOperatorInvoice(invoice.id);
      onUpdated(updated);
      const { key, tone } = pressToast(updated);
      toast[tone](t(key));
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t(action === "check" ? "invoicingCheckFailed" : "invoicingResendFailed"),
      );
    } finally {
      setBusy(null);
    }
  }

  async function annul() {
    setConfirmingAnnulment(false);
    setBusy("annul");
    setError(null);
    try {
      onUpdated(await annulOperatorInvoice(invoice.id));
      toast.success(t("invoicingMarkAnnulledDone"));
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t("invoicingMarkAnnulledFailed"),
      );
    } finally {
      setBusy(null);
    }
  }

  async function abandon() {
    setConfirmingAbandonment(false);
    setBusy("abandon");
    setError(null);
    try {
      onUpdated(await abandonOperatorInvoice(invoice.id, abandonNote.trim() === "" ? null : abandonNote.trim()));
      toast.success(t("invoicingAbandonDone"));
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t("invoicingAbandonFailed"),
      );
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingActionsTitle")}</CardTitle>
        <CardDescription>{t("invoicingActionsDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {invoice.check_status_hint ? (
          <Alert variant="warning">
            <AlertTitle>{t("invoicingCheckStatusHintTitle")}</AlertTitle>
            <AlertDescription>{t("invoicingCheckStatusHint")}</AlertDescription>
          </Alert>
        ) : null}
        {resendAskedAt !== null ? (
          <Alert variant="warning">
            <AlertTitle>{t("invoicingResendHintTitle")}</AlertTitle>
            <AlertDescription>
              {t("invoicingResendHint", {
                time: formatDateTime(resendAskedAt, PLATFORM_TIME_ZONE, locale) ?? resendAskedAt,
              })}
            </AlertDescription>
          </Alert>
        ) : null}
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <div className="flex flex-wrap gap-2">
          {levers.check ? (
            <Button type="button" disabled={busy !== null} onClick={() => void run("check")}>
              {busy === "check" ? t("invoicingCheckingStatus") : t("invoicingCheckStatus")}
            </Button>
          ) : null}
          {levers.resend ? (
            <Button type="button" variant="outline" disabled={busy !== null} onClick={() => void run("resend")}>
              {busy === "resend" ? t("invoicingResending") : t("invoicingResend")}
            </Button>
          ) : null}
          {levers.annul ? (
            <Button
              type="button"
              variant="destructive"
              disabled={busy !== null}
              onClick={() => setConfirmingAnnulment(true)}
            >
              {busy === "annul" ? t("invoicingMarkingAnnulled") : t("invoicingMarkAnnulled")}
            </Button>
          ) : null}
          {levers.abandon ? (
            <Button
              type="button"
              variant="destructive"
              disabled={busy !== null}
              onClick={() => setConfirmingAbandonment(true)}
            >
              {busy === "abandon" ? t("invoicingAbandoning") : t("invoicingAbandon")}
            </Button>
          ) : null}
        </div>
        {levers.abandon ? (
          // Why Mark annulled is gone and what stands in its place (#578,
          // ADR 0068). The operator reaching for it on a refused-by-number
          // document is reaching for the one act that would be a falsehood,
          // and a lever that simply vanishes teaches nothing.
          <p className="text-xs text-muted-foreground">{t("invoicingAnnulRefusedByNumber")}</p>
        ) : null}
        {levers.abandon ? <p className="text-xs text-muted-foreground">{t("invoicingAbandonHint")}</p> : null}
        {invoice.refused_by_number ? (
          // Why Resend is not among the buttons (#577, ADR 0068). The API
          // refuses it with INVOICE_REFUSED_BY_NUMBER, so offering it would
          // be offering a certain failure — but a lever that simply vanishes
          // teaches nothing, and the operator who resent 001-001-000000025
          // for two days is exactly the reader this sentence is for. Check
          // status is still there, and still worth pressing.
          <p className="text-xs text-muted-foreground">{t("invoicingResendRefusedByNumber")}</p>
        ) : null}
        {levers.annul ? <p className="text-xs text-muted-foreground">{t("invoicingMarkAnnulledHint")}</p> : null}
      </CardContent>

      <Dialog open={confirmingAbandonment} onOpenChange={setConfirmingAbandonment}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("invoicingAbandonConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("invoicingAbandonConfirm")}</DialogDescription>
          </DialogHeader>
          {/*
            The note travels with the record and is the operator's own words
            about why — "not registered at the portal, confirmed by phone".
            Optional, and bounded by the same column the reissue note uses.
          */}
          <FormField id="abandon_note" label={t("invoicingAbandonNoteLabel")}>
            <Textarea
              value={abandonNote}
              maxLength={NOTE_MAX_LENGTH}
              rows={3}
              placeholder={t("invoicingAbandonNotePlaceholder")}
              onChange={(event) => setAbandonNote(event.target.value)}
            />
          </FormField>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingAbandonment(false)}>
              {t("invoicingAbandonCancel")}
            </Button>
            <Button type="button" variant="destructive" onClick={() => void abandon()}>
              {t("invoicingAbandon")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmingAnnulment} onOpenChange={setConfirmingAnnulment}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("invoicingMarkAnnulledConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("invoicingMarkAnnulledConfirm")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingAnnulment(false)}>
              {t("invoicingMarkAnnulledCancel")}
            </Button>
            <Button type="button" variant="destructive" onClick={() => void annul()}>
              {t("invoicingMarkAnnulled")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
