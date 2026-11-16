"use client";

import { useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  Textarea,
  cn,
  toast,
} from "@ticket-pos/ui";
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import {
  annulmentPanelMode,
  annulmentRecordReady,
  annulmentWindowProgress,
  annulmentWindowTone,
} from "@/lib/annulment-panel";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatCalendarDay, formatDateTime } from "@/lib/format";
import {
  type OperatorAnnulmentWindow,
  type OperatorInvoiceDetail,
  annulOperatorInvoice,
} from "@/lib/operator-api";

import { INVOICE_STATUS_KEYS } from "../invoice-status";

/**
 * The annulment panel (#745, spec #742, ADR 0078): Mark annulled on a factura,
 * at the top of its page above the Recipient, on every factura the act admits.
 *
 * ONE ACT, ALWAYS LIVE. The SRI offers no web service for annulment: the
 * operator annuls at the SRI en linea portal and records it here, whether the
 * portal step came before or after. So the record form is never behind a
 * button or a step - an operator who already annulled records it in one go.
 * It replaces the SRI-remedies card's Mark annulled and its confirm for every
 * factura; a Credit Note keeps that button exactly as it was.
 *
 * THE WINDOW IS THE API'S. "Annullable until <date>", in a warning tone from
 * day 1 of the following month, and a timeline from the Emission Date to the
 * deadline with today marked, greyed once closed: every date, the days left and
 * whether it is open come from `annulability.window`, computed in Guayaquil by
 * the backend, so this page never decides a date. Past the deadline the form
 * stays, with the acknowledgement field, because a rollover-day annulment is
 * real.
 *
 * THE CONFIRM is the document's number typed in full, an optional note, and
 * past the deadline the acknowledgement typed as a phrase. It says what the
 * platform cannot see - a tax-refund factura, or one already declared, is not
 * annulled by the SRI - so the operator answers for it.
 *
 * A REFUSED FACTURA'S PANEL is one sentence saying why (a live Credit Note, a
 * live Reissue, a number the SRI refuses), with no form: an act certain to
 * fail is not offered. After the act the panel reads "Annulled at the SRI"
 * with its trail: who, when, the note and the late acknowledgement.
 *
 * The portal checklist and its copy buttons are a later ticket; the portal
 * fields are already in `annulability.portal` for it.
 */
export function OperatorInvoiceAnnulment({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const mode = annulmentPanelMode(invoice);
  if (mode === null) {
    return null;
  }
  if (mode === "annulled") {
    return <AnnulledPanel invoice={invoice} />;
  }
  if (mode === "refused") {
    return <RefusedPanel invoice={invoice} />;
  }
  return <RecordPanel invoice={invoice} onUpdated={onUpdated} />;
}

function RefusedPanel({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const refusal = invoice.annulability?.refusal;
  if (!refusal) {
    return null;
  }
  // The one sentence is the API's refusal in the reader's language, keyed on
  // its code exactly as a refused press would be shown.
  const sentence =
    apiErrorMessage(errorCopy, new ApiError(refusal.message, refusal.code)) ?? refusal.message;
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle>{t("invoicingAnnulPanelRefusedTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="text-sm text-muted-foreground">{sentence}</CardContent>
    </Card>
  );
}

function AnnulledPanel({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const current = invoice.annulment?.current ?? null;
  // A factura annulled before the annulments ledger existed (#477) has no
  // record there; its trail is the row's own pair.
  const by = current?.annulled_by ?? invoice.annulled_by;
  const at = current?.annulled_at ?? invoice.annulled_at;
  return (
    <Card className="border-l-4 border-l-neutral-500">
      <CardHeader className="pb-3">
        <CardTitle>{t("invoicingAnnulPanelAnnulledTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm text-muted-foreground">
        {by && at ? (
          <p>
            {t("invoicingAnnulPanelTrail", {
              by,
              when: formatDateTime(at, PLATFORM_TIME_ZONE, locale) ?? at,
            })}
          </p>
        ) : null}
        {current ? (
          <p>{t("invoicingAnnulPanelTrailFrom", { status: t(INVOICE_STATUS_KEYS[current.from_status]) })}</p>
        ) : null}
        {current?.note ? (
          <p>
            <span className="font-medium text-foreground">{t("invoicingAnnulPanelTrailNote")}</span>{" "}
            <span className="whitespace-pre-wrap">{current.note}</span>
          </p>
        ) : null}
        {current?.late_acknowledged ? <p>{t("invoicingAnnulPanelTrailLate")}</p> : null}
      </CardContent>
    </Card>
  );
}

function WindowTimeline({ annulWindow, locale }: { annulWindow: OperatorAnnulmentWindow; locale: AppLocale }) {
  const t = useTranslations("operator");
  const tone = annulmentWindowTone(annulWindow);
  const pct = Math.round(annulmentWindowProgress(annulWindow) * 100);
  const daysLabel =
    tone === "closed"
      ? t("invoicingAnnulPanelWindowClosed")
      : annulWindow.days_left === 0
        ? t("invoicingAnnulPanelLastDay")
        : t("invoicingAnnulPanelDaysLeft", { days: annulWindow.days_left });
  return (
    <div className="space-y-1">
      <div className="relative h-2 rounded-full bg-muted">
        <div
          className={cn("absolute inset-y-0 left-0 rounded-full", TONE_BAR[tone])}
          style={{ width: `${pct}%` }}
        />
        <div className="absolute -top-1 h-4 w-0.5 bg-foreground" style={{ left: `calc(${pct}% - 1px)` }} />
      </div>
      <div className="flex justify-between gap-2 text-xs text-muted-foreground">
        <span>{t("invoicingAnnulPanelIssued", { date: formatCalendarDay(annulWindow.issued_on, locale) })}</span>
        <span className={cn(tone === "warning" && "font-medium text-amber-700")}>{daysLabel}</span>
        <span>{t("invoicingAnnulPanelDeadline", { date: formatCalendarDay(annulWindow.deadline, locale) })}</span>
      </div>
    </div>
  );
}


// The window's tone, as the timeline's bar and the card's edge both draw it.
const TONE_BAR = { open: "bg-emerald-500", warning: "bg-amber-500", closed: "bg-neutral-400" } as const;
const TONE_EDGE = { open: "border-l-emerald-500", warning: "border-l-amber-500", closed: "border-l-neutral-400" } as const;

// The note is bounded exactly as the Abandon's and the reissue's are.
const NOTE_MAX_LENGTH = 500;

function RecordPanel({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const errorCopy = useMessages().errors;
  const [typedNumber, setTypedNumber] = useState("");
  const [typedAcknowledgement, setTypedAcknowledgement] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const annulWindow = invoice.annulability?.window ?? null;
  const number = invoice.number;
  if (annulWindow === null || number === null) {
    return null;
  }
  const tone = annulmentWindowTone(annulWindow);
  const deadline = formatCalendarDay(annulWindow.deadline, locale);
  const late = !annulWindow.open;
  const phrase = t("invoicingAnnulPanelLatePhrase");
  const ready = annulmentRecordReady({ number, typedNumber, late, typedAcknowledgement, phrase });

  async function record() {
    if (number === null) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onUpdated(
        await annulOperatorInvoice(invoice.id, {
          number: typedNumber.trim(),
          note: note.trim() === "" ? null : note.trim(),
          late_acknowledged: late,
        }),
      );
      toast.success(t("invoicingAnnulPanelDone"));
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t("invoicingAnnulPanelFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className={cn("border-l-4", TONE_EDGE[tone])}>
      <CardHeader className="space-y-3 pb-3">
        <CardTitle>{t("invoicingAnnulPanelTitle")}</CardTitle>
        <WindowTimeline annulWindow={annulWindow} locale={locale} />
        <p
          className={cn(
            "text-sm",
            tone === "warning" ? "font-medium text-amber-700" : "text-muted-foreground",
          )}
        >
          {!annulWindow.open
            ? t("invoicingAnnulPanelClosed", { deadline })
            : annulWindow.days_left === 0
              ? t("invoicingAnnulPanelUntilToday", { deadline })
              : t("invoicingAnnulPanelUntil", { deadline, days: annulWindow.days_left })}
        </p>
      </CardHeader>
      <CardContent>
        <div className="space-y-3 rounded-lg bg-muted/40 p-4">
          <p className="text-sm font-medium">{t("invoicingAnnulPanelRecordTitle")}</p>
          <p className="text-xs text-muted-foreground">{t("invoicingAnnulPanelRecordHint")}</p>
          {error ? (
            <Alert variant="destructive">
              <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <FormField id="annul_number" label={t("invoicingAnnulPanelNumberLabel", { number })}>
            <Input
              value={typedNumber}
              placeholder={number}
              autoComplete="off"
              className="font-mono"
              onChange={(event) => setTypedNumber(event.target.value)}
            />
          </FormField>
          {late ? (
            <FormField id="annul_late" label={t("invoicingAnnulPanelLateLabel", { phrase })}>
              <Input
                value={typedAcknowledgement}
                autoComplete="off"
                onChange={(event) => setTypedAcknowledgement(event.target.value)}
              />
            </FormField>
          ) : null}
          <FormField id="annul_note" label={t("invoicingAnnulPanelNoteLabel")}>
            <Textarea
              value={note}
              maxLength={NOTE_MAX_LENGTH}
              rows={2}
              placeholder={t("invoicingAnnulPanelNotePlaceholder")}
              onChange={(event) => setNote(event.target.value)}
            />
          </FormField>
          <p className="text-xs text-muted-foreground">{t("invoicingAnnulPanelReminder")}</p>
          <Button
            type="button"
            variant="destructive"
            className="w-full"
            disabled={!ready || busy}
            onClick={() => void record()}
          >
            {busy ? t("invoicingAnnulPanelSubmitting") : t("invoicingAnnulPanelSubmit")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
