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
  Input,
  Textarea,
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type InvoiceRecipient, type OperatorInvoiceDetail, reissueOperatorInvoice } from "@/lib/operator-api";

/**
 * The Sale Invoice Reissue (#483, ADR 0061): the one thing a Platform
 * Operator can do to an AUTHORIZED Sale Invoice — correct its Recipient.
 *
 * SHOWN ONLY WHEN ALLOWED. The page renders this card on
 * `invoiceLevers(...).reissue` alone: a current, authorized Sale Invoice.
 * Everywhere else the API is certain to refuse, and the card is not there.
 *
 * THE FORM IS PREFILLED WITH THE CURRENT RECIPIENT, so the operator corrects
 * only what is wrong. There is no email field: the corrected factura goes
 * to whatever address the Sale carries at that moment, which the API takes
 * itself — a Sale Re-addressing before the reissue is honoured, and the
 * superseded factura keeps the address it was issued to. The note is for a
 * colleague reading the chain later; it is never mailed.
 *
 * THE OUTCOME IS A NEW DOCUMENT. The API answers with the corrected Sale
 * Invoice — a new id, owed and unsigned, worked by the Drainer once its
 * Credit Note is authorized — and the page moves to it; the document that
 * was reissued is a click away through the chain card.
 */

const SELECT_CLASS =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60";

const NOTE_MAX_LENGTH = 500;

export function OperatorInvoiceReissue({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [taxIdType, setTaxIdType] = useState<InvoiceRecipient["tax_id_type"]>(invoice.recipient.tax_id_type);
  const [taxId, setTaxId] = useState(invoice.recipient.tax_id);
  const [legalName, setLegalName] = useState(invoice.recipient.legal_name);
  const [address, setAddress] = useState(invoice.recipient.address);
  const [note, setNote] = useState("");

  function openForm() {
    // Prefilled with the current Recipient every time the form opens, so a
    // cancelled attempt never leaks into the next one.
    setTaxIdType(invoice.recipient.tax_id_type);
    setTaxId(invoice.recipient.tax_id);
    setLegalName(invoice.recipient.legal_name);
    setAddress(invoice.recipient.address);
    setNote("");
    setError(null);
    setFieldErrors({});
    setOpen(true);
  }

  async function submit() {
    setBusy(true);
    setError(null);
    setFieldErrors({});
    try {
      const corrected = await reissueOperatorInvoice(invoice.id, {
        recipient: { tax_id_type: taxIdType, tax_id: taxId, legal_name: legalName, address },
        note: note.trim() === "" ? null : note.trim(),
      });
      setOpen(false);
      toast.success(t("invoicingReissueDone"));
      router.push(`/operator/invoicing/${corrected.id}`);
    } catch (submitError) {
      if (submitError instanceof ApiError && submitError.details) {
        setFieldErrors(fieldErrorMessages(errorCopy, submitError.details));
      }
      setError(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("invoicingReissueFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingReissueCardTitle")}</CardTitle>
        <CardDescription>{t("invoicingReissueCardDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <Button type="button" onClick={openForm}>
          {t("invoicingReissue")}
        </Button>
      </CardContent>

      <Dialog open={open} onOpenChange={(next) => (busy ? null : setOpen(next))}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("invoicingReissueDialogTitle")}</DialogTitle>
            <DialogDescription>{t("invoicingReissueDialogDescription")}</DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            {error ? (
              <Alert variant="destructive">
                <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                id="reissue_tax_id_type"
                label={t("invoicingRecipientTaxIdType")}
                error={fieldErrors["recipient.tax_id_type"]}
              >
                <select
                  className={SELECT_CLASS}
                  value={taxIdType}
                  disabled={busy}
                  onChange={(event) => setTaxIdType(event.target.value as InvoiceRecipient["tax_id_type"])}
                >
                  <option value="ruc">{t("invoicingRecipientTaxIdTypeRuc")}</option>
                  <option value="cedula">{t("invoicingRecipientTaxIdTypeCedula")}</option>
                  <option value="passport">{t("invoicingRecipientTaxIdTypePassport")}</option>
                </select>
              </FormField>
              <FormField id="reissue_tax_id" label={t("invoicingRecipientTaxId")} error={fieldErrors["recipient.tax_id"]}>
                <Input value={taxId} disabled={busy} onChange={(event) => setTaxId(event.target.value)} />
              </FormField>
              <FormField
                id="reissue_legal_name"
                label={t("invoicingRecipientLegalName")}
                error={fieldErrors["recipient.legal_name"]}
                className="sm:col-span-2"
              >
                <Input value={legalName} disabled={busy} onChange={(event) => setLegalName(event.target.value)} />
              </FormField>
              <FormField
                id="reissue_address"
                label={t("invoicingRecipientAddress")}
                error={fieldErrors["recipient.address"]}
                className="sm:col-span-2"
              >
                <Input value={address} disabled={busy} onChange={(event) => setAddress(event.target.value)} />
              </FormField>
              <FormField
                id="reissue_email"
                label={t("invoicingRecipientEmail")}
                description={t("invoicingReissueEmailHint")}
                className="sm:col-span-2"
              >
                <Input value={invoice.recipient.email} disabled readOnly />
              </FormField>
              <FormField
                id="reissue_note"
                label={t("invoicingReissueNote")}
                description={t("invoicingReissueNoteHint")}
                error={fieldErrors["note"]}
                className="sm:col-span-2"
              >
                <Textarea
                  value={note}
                  disabled={busy}
                  maxLength={NOTE_MAX_LENGTH}
                  rows={3}
                  onChange={(event) => setNote(event.target.value)}
                />
              </FormField>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setOpen(false)}>
                {t("invoicingReissueCancel")}
              </Button>
              <Button type="submit" disabled={busy}>
                {busy ? t("invoicingReissuing") : t("invoicingReissue")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
