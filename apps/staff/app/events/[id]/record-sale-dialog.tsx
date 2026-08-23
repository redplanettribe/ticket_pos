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
import { ApiError, isoToDateTimeLocal, type TicketType } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatCalendarDay, formatMoney } from "@/lib/format";
import {
  MANUAL_SALE_COLUMNS,
  emptyManualSaleForm,
  manualSaleBody,
  manualSaleVerdict,
  type ManualSaleForm,
  type ManualSaleVerdict,
} from "@/lib/manual-sale";
import {
  TAX_ID_TYPES,
  previewManualSale,
  recordSale,
  rowFieldErrors,
  type RecordedSale,
} from "@/lib/sales-api";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

// How long the form waits after the last keystroke before asking for a
// verdict: long enough not to judge every character of an email, short
// enough that the answer is there by the time the eye reaches the footer.
// The Sale Correction's own figure — the two forms judge the same row.
const PREVIEW_DEBOUNCE_MS = 400;

type RecordSaleDialogProps = {
  eventId: string;
  open: boolean;
  /** The Event's Ticket Type catalog: the sale's type is PICKED, never typed. */
  ticketTypes: TicketType[];
  /** The Organization's currency, for the prices beside the Ticket Types. */
  currency: string;
  /**
   * The Event's own timezone, which the sale date is typed and defaulted in.
   * Null on an Event that names none, and then the platform's clock beneath it
   * — never the reader's laptop (#289, ADR 0041).
   */
  timezone: string | null;
  onClose: () => void;
  /** Called after a sale is recorded, with what the API says it recorded. */
  onRecorded: (sale: RecordedSale) => void;
};

/**
 * The record-a-sale form (#369, ADR 0052): one off-platform Ticket Sale typed
 * instead of uploaded, carrying the Sale Import template's columns cell for
 * cell so nobody has to learn a second vocabulary for the same act.
 *
 * It judges nothing itself. Every complaint on screen came from the preview
 * endpoint or from the save's own refusal — the app has no validation library
 * and adds none — and each lands on the field the API blamed.
 *
 * There is no Keep adding here; the sitting is #372 and wraps this form rather
 * than living inside it, which is why the form's state, its body and its
 * verdict are pure functions in lib/manual-sale.ts.
 */
export function RecordSaleDialog({
  eventId,
  open,
  ticketTypes,
  currency,
  timezone,
  onClose,
  onRecorded,
}: RecordSaleDialogProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const zone = timezone ?? PLATFORM_TIME_ZONE;
  const [form, setForm] = useState<ManualSaleForm | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  // The live verdict: null until the first answer arrives, and `checking` while
  // an edit's answer is on its way. A verdict that refuses disables the save; a
  // duplicate warning only says so.
  const [verdict, setVerdict] = useState<ManualSaleVerdict | null>(null);
  const [checking, setChecking] = useState(false);
  const [previewFailed, setPreviewFailed] = useState(false);
  // Whether anything has been typed yet. A blank form is refused by every rule
  // there is, and asking for that verdict on open would greet the Organizer
  // with eight complaints about fields they have not reached. The form judges
  // itself AS they type, so the first keystroke is when it starts.
  const [typed, setTyped] = useState(false);
  // Which preview request is the latest, so a slow earlier answer cannot
  // overwrite the verdict on what has since been typed.
  const previewSeq = useRef(0);

  // A blank form every time the modal opens, dated today on the EVENT's clock:
  // recording the sale just taken should need no thought, and "today" is a
  // question only the Event's timezone may answer.
  useEffect(() => {
    setForm(open ? emptyManualSaleForm(isoToDateTimeLocal(new Date().toISOString(), zone)) : null);
    setFieldErrors({});
    setVerdict(null);
    setPreviewFailed(false);
    setTyped(false);
  }, [open, zone]);

  // Ask for the verdict as the Organizer types, debounced, on exactly the body
  // the save would send. The preview's complaints take the field slots the
  // save's refusal would, so the form reads the same before and after.
  useEffect(() => {
    if (!open || !form || !typed) {
      return;
    }
    const seq = ++previewSeq.current;
    setChecking(true);
    const timer = setTimeout(async () => {
      try {
        const result = await previewManualSale(eventId, manualSaleBody(form, zone));
        if (seq !== previewSeq.current) {
          return;
        }
        const next = manualSaleVerdict(result);
        setVerdict(next);
        setFieldErrors(next.fieldErrors);
        setPreviewFailed(false);
      } catch (error) {
        if (seq !== previewSeq.current) {
          return;
        }
        // The preview answers 200 whatever the verdict, with one exception: a
        // quantity that is not a whole number never reaches the validator, so
        // it arrives as a refusal instead of as a row. It is a complaint about
        // a column like any other, and is shown on that column.
        const fields =
          error instanceof ApiError && error.code === "VALIDATION_FAILED"
            ? rowFieldErrors(error.details)
            : {};
        if (Object.keys(fields).length > 0) {
          setVerdict({ blocks: true, fieldErrors: fields, duplicateOfDate: null, remaining: null });
          setFieldErrors(fields);
          setPreviewFailed(false);
          return;
        }
        // The save will say what is wrong; the form only stops claiming to know
        // the verdict ahead of it.
        setVerdict(null);
        setPreviewFailed(true);
      } finally {
        if (seq === previewSeq.current) {
          setChecking(false);
        }
      }
    }, PREVIEW_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [eventId, open, form, typed, zone]);

  function set<K extends keyof ManualSaleForm>(key: K, value: ManualSaleForm[K]) {
    setTyped(true);
    setForm((current) => (current ? { ...current, [key]: value } : current));
  }

  function errorFor(field: keyof ManualSaleForm): string | null {
    return fieldErrors[MANUAL_SALE_COLUMNS[field]] ?? null;
  }

  /**
   * Records the sale. A failure leaves every typed value where it is — a
   * refusal costs a fix, not a retype — and the button is disabled for the
   * whole flight, which is what stops a double-click recording twice: there is
   * no idempotency key behind this create (ADR 0052).
   */
  async function submit() {
    if (!form || saving || verdict?.blocks) {
      return;
    }
    setSaving(true);
    setFieldErrors({});
    try {
      const recorded = await recordSale(eventId, manualSaleBody(form, zone));
      toast.success(t("recordDone", { reference: recorded.confirmation_ref }));
      onRecorded(recorded);
    } catch (error) {
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        const fields = rowFieldErrors(error.details);
        setFieldErrors(fields);
        if (Object.keys(fields).length === 0) {
          toast.error(apiErrorMessage(errorCopy, error) ?? t("recordFailed"));
        }
      } else {
        toast.error(
          (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
            t("recordFailed"),
        );
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => (!isOpen && !saving ? onClose() : undefined)}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("recordTitle")}</DialogTitle>
          <DialogDescription>{t("recordBody")}</DialogDescription>
        </DialogHeader>
        {form ? (
          <form
            className="grid gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            <Field id="record-email" label={t("recordEmail")} error={errorFor("customerEmail")}>
              <Input
                id="record-email"
                type="email"
                value={form.customerEmail}
                onChange={(event) => set("customerEmail", event.target.value)}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="record-first-name"
                label={t("recordFirstName")}
                error={errorFor("customerFirstName")}
              >
                <Input
                  id="record-first-name"
                  value={form.customerFirstName}
                  onChange={(event) => set("customerFirstName", event.target.value)}
                />
              </Field>
              <Field
                id="record-last-name"
                label={t("recordLastName")}
                error={errorFor("customerLastName")}
              >
                <Input
                  id="record-last-name"
                  value={form.customerLastName}
                  onChange={(event) => set("customerLastName", event.target.value)}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="record-tax-type"
                label={t("recordTaxIdType")}
                error={errorFor("taxIdType")}
              >
                <select
                  id="record-tax-type"
                  className={SELECT_CLASS}
                  value={form.taxIdType}
                  onChange={(event) => set("taxIdType", event.target.value)}
                >
                  <option value="">{t("recordTaxIdNone")}</option>
                  {TAX_ID_TYPES.map((type) => (
                    <option key={type} value={type}>
                      {t(TAX_ID_OPTION_KEYS[type])}
                    </option>
                  ))}
                </select>
              </Field>
              <Field
                id="record-tax-number"
                label={t("recordTaxIdNumber")}
                description={t("recordTaxIdHint")}
                error={errorFor("taxIdNumber")}
              >
                <Input
                  id="record-tax-number"
                  value={form.taxIdNumber}
                  onChange={(event) => set("taxIdNumber", event.target.value)}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                id="record-ticket-type"
                label={t("recordTicketType")}
                error={errorFor("ticketTypeId")}
              >
                <select
                  id="record-ticket-type"
                  className={SELECT_CLASS}
                  value={form.ticketTypeId}
                  onChange={(event) => set("ticketTypeId", event.target.value)}
                >
                  <option value="">{t("recordTicketTypeChoose")}</option>
                  {/* A Ticket Type's name is data and goes in as it was coined;
                      its price is drawn in the Organization's currency. */}
                  {ticketTypes.map((type) => (
                    <option key={type.id} value={type.id}>
                      {t("recordTicketTypeOption", {
                        name: type.name,
                        price: formatMoney(type.price_cents, type.currency || currency, locale),
                      })}
                    </option>
                  ))}
                </select>
              </Field>
              <Field id="record-quantity" label={t("recordQuantity")} error={errorFor("quantity")}>
                <Input
                  id="record-quantity"
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
                id="record-payment-method"
                label={t("recordPaymentMethod")}
                error={errorFor("paymentMethod")}
              >
                <select
                  id="record-payment-method"
                  className={SELECT_CLASS}
                  value={form.paymentMethod}
                  onChange={(event) => set("paymentMethod", event.target.value)}
                >
                  <option value="">{t("recordPaymentMethodChoose")}</option>
                  <option value="cash">{t("paymentCash")}</option>
                  <option value="transfer">{t("paymentTransfer")}</option>
                </select>
              </Field>
              <Field
                id="record-sold-at"
                label={t("recordSoldAt", { timezone: zone })}
                error={errorFor("soldAtLocal")}
              >
                <Input
                  id="record-sold-at"
                  type="datetime-local"
                  value={form.soldAtLocal}
                  onChange={(event) => set("soldAtLocal", event.target.value)}
                />
              </Field>
            </div>
            <Field
              id="record-unit-price"
              label={t("recordUnitPrice")}
              description={t("recordUnitPriceHint")}
              error={errorFor("unitPrice")}
            >
              <Input
                id="record-unit-price"
                type="number"
                min="0"
                step="0.01"
                value={form.unitPrice}
                onChange={(event) => set("unitPrice", event.target.value)}
              />
            </Field>

            <VerdictLine verdict={verdict} checking={checking} failed={previewFailed} />

            <DialogFooter>
              <Button type="button" variant="outline" disabled={saving} onClick={onClose}>
                {t("recordCancel")}
              </Button>
              <Button type="submit" disabled={saving || checking || verdict?.blocks === true}>
                {saving ? t("recording") : t("recordConfirm")}
              </Button>
            </DialogFooter>
          </form>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

/**
 * What the verdict says beneath the form: that it is being checked, that the
 * sale would be refused (the complaints sit on the fields), that it looks like
 * a sale the Event already has (a warning, the Organizer's call), or that it
 * fits and what the Ticket Type has left afterwards.
 */
function VerdictLine({
  verdict,
  checking,
  failed,
}: {
  verdict: ManualSaleVerdict | null;
  checking: boolean;
  failed: boolean;
}) {
  const t = useTranslations("sales");
  const locale = toAppLocale(useLocale());
  if (checking) {
    return (
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {t("recordVerdictChecking")}
      </p>
    );
  }
  if (failed) {
    return (
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {t("recordVerdictUnavailable")}
      </p>
    );
  }
  if (!verdict) {
    return null;
  }
  if (verdict.blocks) {
    return (
      <p className="text-sm text-destructive" role="alert">
        {t("recordVerdictRefused")}
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
          {t("recordVerdictDuplicate", { date: duplicateDate })}
        </p>
      ) : null}
      <p className="text-muted-foreground">
        {verdict.remaining
          ? t("recordVerdictFits", {
              remaining: verdict.remaining.remaining,
              ticketType: verdict.remaining.ticketTypeName,
            })
          : t("recordVerdictFitsPlain")}
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
