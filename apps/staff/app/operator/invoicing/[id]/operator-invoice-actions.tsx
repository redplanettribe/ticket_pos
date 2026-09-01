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
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { hasInvoiceLevers, invoiceLevers } from "@/lib/invoice-actions";
import {
  type InvoiceStatus,
  type OperatorInvoiceDetail,
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
 * authorization and the ledger from it; the toast only says what changed.
 * WHO HOLDS THE DOCUMENT is what the two hints say, and they are never both
 * true (#516). When the SRI holds it and is still deciding — after a plain
 * RECIBIDA, or after a resend the SRI met with "clave already registered"
 * (43) or "in processing" (70) — the API says so with `check_status_hint`,
 * and the card shows "it is there, check status" instead of an error. When
 * the SRI answered that it has no record of the clave and never took the
 * document — where a submit that died in transport leaves it — `resend_hint`
 * says so instead, and the card asks for the resend that is the only thing
 * that would fix it. A document the ledger says neither about shows neither
 * banner: the card offers its levers and says nothing it cannot know.
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
 * so it is confirmed first. Which levers show is `invoiceLevers`' decision.
 * Reissue (#483) is that decision's fourth lever and its own card
 * (operator-invoice-reissue.tsx): it belongs to an authorized document,
 * which this card — the SRI's remedies — never renders for.
 */

const OUTCOME_KEYS = {
  authorized: "invoicingCheckedAuthorized",
  not_authorized: "invoicingCheckedNotAuthorized",
  rejected: "invoicingCheckedRejected",
  pending: "invoicingCheckedPending",
} as const satisfies Partial<Record<InvoiceStatus, string>>;

export function OperatorInvoiceActions({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const [busy, setBusy] = useState<"check" | "resend" | "annul" | null>(null);
  const [confirmingAnnulment, setConfirmingAnnulment] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const levers = invoiceLevers(invoice.status, invoice.ecuador !== null, undefined, invoice.refused_by_number);
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
      if (updated.status === "authorized") {
        toast.success(t(OUTCOME_KEYS.authorized));
      } else {
        toast.info(t(OUTCOME_KEYS[updated.status as keyof typeof OUTCOME_KEYS] ?? "invoicingCheckedPending"));
      }
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
        {invoice.resend_hint ? (
          <Alert variant="warning">
            <AlertTitle>{t("invoicingResendHintTitle")}</AlertTitle>
            <AlertDescription>{t("invoicingResendHint")}</AlertDescription>
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
        </div>
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
