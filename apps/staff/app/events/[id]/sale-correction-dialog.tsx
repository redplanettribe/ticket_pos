"use client";

import { useEffect, useRef, useState } from "react";

import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  toast,
} from "@ticket-pos/ui";
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, dateTimeLocalToISO, isoToDateTimeLocal } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatCalendarDay } from "@/lib/format";
import {
  TAX_ID_TYPES,
  correctSale,
  correctionFieldErrors,
  correctionPrefill,
  correctionVerdict,
  previewSaleCorrection,
  type CorrectSaleInput,
  type CorrectionVerdict,
  type SaleListRow,
} from "@/lib/sales-api";

import type { TicketTypeOption } from "./sales-list";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

/**
 * The form's own state: every template column as typed, which is what the API
 * judges. The sold-at is held as a datetime-local string in the Event's zone
 * and the unit price in major units, because that is what the inputs hold;
 * both are converted on submit and never before.
 */
type CorrectionForm = {
  customerEmail: string;
  customerFirstName: string;
  customerLastName: string;
  taxIdType: string;
  taxIdNumber: string;
  ticketTypeId: string;
  quantity: string;
  paymentMethod: string;
  soldAtLocal: string;
  unitPrice: string;
  sendConfirmation: boolean;
};

function formFrom(sale: SaleListRow, zone: string): CorrectionForm {
  const prefill = correctionPrefill(sale);
  // The row carries the sale's TOTAL; the API's amount is per unit (the
  // import's own convention), so the form shows the unit price and the
  // total is restated beneath it from quantity × unit.
  const unit = prefill.quantity > 0 ? prefill.amount_cents! / prefill.quantity : null;
  return {
    customerEmail: prefill.customer_email,
    customerFirstName: prefill.customer_first_name,
    customerLastName: prefill.customer_last_name,
    taxIdType: prefill.customer_tax_id_type,
    taxIdNumber: prefill.customer_tax_id_number,
    ticketTypeId: prefill.ticket_type_id,
    quantity: String(prefill.quantity),
    paymentMethod: prefill.payment_method,
    soldAtLocal: isoToDateTimeLocal(prefill.sold_at, zone),
    unitPrice: unit !== null && Number.isInteger(unit) ? (unit / 100).toFixed(2) : "",
    sendConfirmation: false,
  };
}

/**
 * toInput is the one reading of the form the API sees — the preview and the
 * commit send exactly the same body, so the verdict shown is the verdict
 * committed on (#352).
 */
function toInput(form: CorrectionForm, zone: string): CorrectSaleInput {
  const quantity = Number.parseInt(form.quantity, 10);
  const price = form.unitPrice.trim() === "" ? null : Number.parseFloat(form.unitPrice);
  return {
    customer_email: form.customerEmail.trim(),
    customer_first_name: form.customerFirstName.trim(),
    customer_last_name: form.customerLastName.trim(),
    customer_tax_id_type: form.taxIdType,
    customer_tax_id_number: form.taxIdNumber.trim(),
    ticket_type_id: form.ticketTypeId,
    quantity: Number.isFinite(quantity) ? quantity : 0,
    payment_method: form.paymentMethod,
    sold_at: dateTimeLocalToISO(form.soldAtLocal, zone) ?? "",
    amount_cents: price !== null && Number.isFinite(price) ? Math.round(price * 100) : null,
    send_confirmation: form.sendConfirmation,
  };
}

// How long the form waits after the last keystroke before asking for a
// verdict: long enough not to judge every character of an email, short
// enough that the answer is there by the time the eye reaches the footer.
const PREVIEW_DEBOUNCE_MS = 400;

/** The template column a field error arrives under, per form field. */
const FIELD_COLUMNS = {
  customerEmail: "customer_email",
  customerFirstName: "customer_first_name",
  customerLastName: "customer_last_name",
  taxIdType: "customer_tax_id_type",
  taxIdNumber: "customer_tax_id_number",
  ticketTypeId: "ticket_type",
  quantity: "quantity",
  paymentMethod: "payment_method",
  soldAtLocal: "sold_at",
  unitPrice: "amount",
} as const;

type SaleCorrectionDialogProps = {
  eventId: string;
  // The sale being corrected, or null while the dialog is closed.
  sale: SaleListRow | null;
  ticketTypes: TicketTypeOption[];
  timezone: string | null;
  onClose: () => void;
  // Called after a successful correction, with the replacement's reference.
  onCorrected: () => void;
};

/**
 * The Sale Correction form (#351, ADR 0050): pre-filled from the row, so the
 * Member retypes only what was wrong. Submitting reverses the sale and records
 * the replacement in one act; the old row stays as "Corrected → TP-X".
 *
 * Three things the copy must say before the press: how many accepted Holders
 * lose their Ticket and are told (always), that the buyer is mailed nothing
 * unless the box is ticked, and that until it is ticked the buyer's old
 * Confirmation Link is dead — the cost ADR 0050 accepts, and the reason the
 * checkbox exists.
 */
export function SaleCorrectionDialog({
  eventId,
  sale,
  ticketTypes,
  timezone,
  onClose,
  onCorrected,
}: SaleCorrectionDialogProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const zone = timezone ?? PLATFORM_TIME_ZONE;
  const [form, setForm] = useState<CorrectionForm | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  // The live verdict (#352): null until the first answer arrives, and
  // `checking` while an edit's answer is on its way. A verdict that refuses
  // disables the commit; a duplicate warning only says so.
  const [verdict, setVerdict] = useState<CorrectionVerdict | null>(null);
  const [checking, setChecking] = useState(false);
  const [previewFailed, setPreviewFailed] = useState(false);
  // Which preview request is the latest, so a slow earlier answer cannot
  // overwrite the verdict on what the Member has since typed.
  const previewSeq = useRef(0);

  // Re-seed from the row every time a sale is chosen, and forget the last
  // attempt's complaints and verdict with it.
  useEffect(() => {
    setForm(sale ? formFrom(sale, zone) : null);
    setFieldErrors({});
    setVerdict(null);
    setPreviewFailed(false);
  }, [sale, zone]);

  // Ask for the verdict as the Member edits, debounced, on exactly the body
  // the commit would send. The preview's complaints take the field slots the
  // commit's refusal would, so the form reads the same before and after.
  useEffect(() => {
    if (!sale || !form) {
      return;
    }
    const seq = ++previewSeq.current;
    setChecking(true);
    const timer = setTimeout(async () => {
      try {
        const result = await previewSaleCorrection(eventId, sale.id, toInput(form, zone));
        if (seq !== previewSeq.current) {
          return;
        }
        const next = correctionVerdict(result);
        setVerdict(next);
        setFieldErrors(next.fieldErrors);
        setPreviewFailed(false);
      } catch {
        if (seq !== previewSeq.current) {
          return;
        }
        // The commit will say what is wrong; the form only stops claiming to
        // know the verdict ahead of it.
        setVerdict(null);
        setPreviewFailed(true);
      } finally {
        if (seq === previewSeq.current) {
          setChecking(false);
        }
      }
    }, PREVIEW_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [eventId, sale, form, zone]);

  function set<K extends keyof CorrectionForm>(key: K, value: CorrectionForm[K]) {
    setForm((current) => (current ? { ...current, [key]: value } : current));
  }

  function errorFor(field: keyof typeof FIELD_COLUMNS): string | null {
    return fieldErrors[FIELD_COLUMNS[field]] ?? null;
  }

  async function submit() {
    if (!sale || !form || verdict?.blocks) {
      return;
    }
    const input = toInput(form, zone);
    setSubmitting(true);
    setFieldErrors({});
    try {
      const result = await correctSale(eventId, sale.id, input);
      toast.success(
        t("correctDone", {
          old: result.reversed_confirmation_ref,
          replacement: result.replacement_confirmation_ref,
        }),
      );
      onCorrected();
    } catch (error) {
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        const fields = correctionFieldErrors(error.details);
        setFieldErrors(fields);
        if (Object.keys(fields).length === 0) {
          toast.error(apiErrorMessage(errorCopy, error) ?? t("correctFailed"));
        }
      } else {
        toast.error(
          (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
            t("correctFailed"),
        );
      }
    } finally {
      setSubmitting(false);
    }
  }

  const open = sale !== null && form !== null;

  return (
    <Dialog open={open} onOpenChange={(isOpen) => (!isOpen && !submitting ? onClose() : undefined)}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("correctTitle")}</DialogTitle>
          <DialogDescription>
            {sale
              ? t("correctBody", {
                  reference: sale.confirmation_ref,
                  count: sale.held_ticket_count,
                })
              : null}
          </DialogDescription>
        </DialogHeader>
        {form ? (
          <form
            className="grid gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            <Field id="correct-email" label={t("correctEmail")} error={errorFor("customerEmail")}>
              <Input
                id="correct-email"
                type="email"
                value={form.customerEmail}
                onChange={(event) => set("customerEmail", event.target.value)}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="correct-first-name"
                label={t("correctFirstName")}
                error={errorFor("customerFirstName")}
              >
                <Input
                  id="correct-first-name"
                  value={form.customerFirstName}
                  onChange={(event) => set("customerFirstName", event.target.value)}
                />
              </Field>
              <Field
                id="correct-last-name"
                label={t("correctLastName")}
                error={errorFor("customerLastName")}
              >
                <Input
                  id="correct-last-name"
                  value={form.customerLastName}
                  onChange={(event) => set("customerLastName", event.target.value)}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field id="correct-tax-type" label={t("correctTaxIdType")} error={errorFor("taxIdType")}>
                <select
                  id="correct-tax-type"
                  className={SELECT_CLASS}
                  value={form.taxIdType}
                  onChange={(event) => set("taxIdType", event.target.value)}
                >
                  <option value="">{t("correctTaxIdNone")}</option>
                  {TAX_ID_TYPES.map((type) => (
                    <option key={type} value={type}>
                      {t(TAX_ID_OPTION_KEYS[type])}
                    </option>
                  ))}
                </select>
              </Field>
              <Field
                id="correct-tax-number"
                label={t("correctTaxIdNumber")}
                error={errorFor("taxIdNumber")}
              >
                <Input
                  id="correct-tax-number"
                  value={form.taxIdNumber}
                  onChange={(event) => set("taxIdNumber", event.target.value)}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="correct-ticket-type"
                label={t("correctTicketType")}
                error={errorFor("ticketTypeId")}
              >
                <select
                  id="correct-ticket-type"
                  className={SELECT_CLASS}
                  value={form.ticketTypeId}
                  onChange={(event) => set("ticketTypeId", event.target.value)}
                >
                  <option value="">{t("correctTicketTypeChoose")}</option>
                  {ticketTypes.map((type) => (
                    <option key={type.id} value={type.id}>
                      {type.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field id="correct-quantity" label={t("correctQuantity")} error={errorFor("quantity")}>
                <Input
                  id="correct-quantity"
                  type="number"
                  min="1"
                  step="1"
                  value={form.quantity}
                  onChange={(event) => set("quantity", event.target.value)}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="correct-payment-method"
                label={t("correctPaymentMethod")}
                error={errorFor("paymentMethod")}
              >
                <select
                  id="correct-payment-method"
                  className={SELECT_CLASS}
                  value={form.paymentMethod}
                  onChange={(event) => set("paymentMethod", event.target.value)}
                >
                  <option value="">{t("correctPaymentMethodChoose")}</option>
                  <option value="cash">{t("paymentCash")}</option>
                  <option value="transfer">{t("paymentTransfer")}</option>
                </select>
              </Field>
              <Field
                id="correct-sold-at"
                label={t("correctSoldAt", { timezone: zone })}
                error={errorFor("soldAtLocal")}
              >
                <Input
                  id="correct-sold-at"
                  type="datetime-local"
                  value={form.soldAtLocal}
                  onChange={(event) => set("soldAtLocal", event.target.value)}
                />
              </Field>
            </div>
            <Field
              id="correct-unit-price"
              label={t("correctUnitPrice")}
              description={t("correctUnitPriceHint")}
              error={errorFor("unitPrice")}
            >
              <Input
                id="correct-unit-price"
                type="number"
                min="0"
                step="0.01"
                value={form.unitPrice}
                onChange={(event) => set("unitPrice", event.target.value)}
              />
            </Field>

            <div className="rounded-md border bg-muted/30 p-3 text-sm">
              <label className="flex items-start gap-2">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={form.sendConfirmation}
                  onChange={(event) => set("sendConfirmation", event.target.checked)}
                />
                <span>
                  <span className="font-medium">{t("correctSendConfirmation")}</span>
                  <span className="mt-1 block text-muted-foreground">
                    {t("correctSendConfirmationHint")}
                  </span>
                </span>
              </label>
            </div>

            <VerdictLine verdict={verdict} checking={checking} failed={previewFailed} />

            <DialogFooter>
              <Button type="button" variant="outline" disabled={submitting} onClick={onClose}>
                {t("correctCancel")}
              </Button>
              <Button type="submit" disabled={submitting || checking || verdict?.blocks === true}>
                {submitting ? t("correcting") : t("correctConfirm")}
              </Button>
            </DialogFooter>
          </form>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

/**
 * What the verdict says beneath the form (#352): that it is being checked,
 * that the replacement would be refused (the complaints sit on the fields),
 * that it looks like another active sale (a warning, the Member's call), or
 * that it fits and what the Ticket Type has left afterwards.
 */
function VerdictLine({
  verdict,
  checking,
  failed,
}: {
  verdict: CorrectionVerdict | null;
  checking: boolean;
  failed: boolean;
}) {
  const t = useTranslations("sales");
  const locale = toAppLocale(useLocale());
  if (checking) {
    return (
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {t("correctVerdictChecking")}
      </p>
    );
  }
  if (failed) {
    return (
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {t("correctVerdictUnavailable")}
      </p>
    );
  }
  if (!verdict) {
    return null;
  }
  if (verdict.blocks) {
    return (
      <p className="text-sm text-destructive" role="alert">
        {t("correctVerdictRefused")}
      </p>
    );
  }
  // A calendar day in the Event's zone, never an instant: the API names the
  // other sale's sold-at DATE, and reading it as UTC midnight would show the
  // day before everywhere this platform sells.
  const duplicateDate = verdict.duplicateOfDate
    ? formatCalendarDay(verdict.duplicateOfDate, locale)
    : null;
  return (
    <div className="grid gap-1 text-sm" aria-live="polite">
      {duplicateDate ? (
        <p className="text-amber-700 dark:text-amber-400" role="status">
          {t("correctVerdictDuplicate", { date: duplicateDate })}
        </p>
      ) : null}
      <p className="text-muted-foreground">
        {verdict.remaining
          ? t("correctVerdictFits", {
              remaining: verdict.remaining.remaining,
              ticketType: verdict.remaining.ticketTypeName,
            })
          : t("correctVerdictFitsPlain")}
      </p>
    </div>
  );
}

const TAX_ID_OPTION_KEYS = {
  cedula: "taxIdCedula",
  ruc: "taxIdRuc",
  passport: "taxIdPassport",
} as const;

function Field({
  id,
  label,
  description,
  error,
  children,
}: {
  id: string;
  label: string;
  description?: string;
  error: string | null;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {description && !error ? (
        <p className="text-xs text-muted-foreground">{description}</p>
      ) : null}
      {error ? (
        <p className="text-xs text-destructive" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
