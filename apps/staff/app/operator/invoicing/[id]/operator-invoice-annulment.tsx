"use client";

import Link from "next/link";
import { useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
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
  ANNULMENT_CHECK_BADGE,
  ANNULMENT_PORTAL_STEPS,
  FOLLOW_UP_CARD_KEYS,
  type FollowUpKind,
  type AnnulmentPortalField,
  type AnnulmentPortalStep,
  SRI_EN_LINEA_URL,
  annulmentCheckLine,
  annulmentCheckOffersCheckStatus,
  annulmentNoticeLine,
  annulmentPanelMode,
  annulmentPortalFields,
  annulmentPortalPathKey,
  annulmentRecordReady,
  annulmentSendsNotice,
  annulmentWindowProgress,
  annulmentWindowTone,
  followUpChooser,
  pastAnnulmentLines,
  reinstateOffer,
  visibleWhitespace,
} from "@/lib/annulment-panel";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatCalendarDay, formatDateTime } from "@/lib/format";
import {
  type OperatorAnnulmentCheck,
  type OperatorAnnulmentNotice,
  type OperatorAnnulmentPortal,
  type OperatorAnnulmentWindow,
  type OperatorInvoiceDetail,
  annulOperatorInvoice,
  checkOperatorInvoice,
} from "@/lib/operator-api";

import { INVOICE_STATUS_KEYS } from "../invoice-status";
import { OperatorInvoiceIssueAgain } from "./operator-invoice-issue-again";
import { OperatorInvoiceReinstate } from "./operator-invoice-reinstate";
import { OperatorInvoiceReissue } from "./operator-invoice-reissue";

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
 * THE ANNULMENT CHECK (#746) sits on the annulled face: its badge, one state
 * line ("checking since 14:02, next ask 14:17", "ANULADO at ...", "still
 * reported as authorized since ...", "SRI didn't confirm at ...; asking every
 * 6 h until ..."), every ConsultaComprobante answer in a collapsed list, and a
 * Check status row whenever the check is not confirmed. The verdict and every
 * time come from `annulment.check`; this page decides only which line to draw
 * (lib/annulment-panel.ts).
 *
 * THE FOLLOW-UP CHOOSER (#747) closes the annulled face: one card per admitted
 * follow-up, each saying what it owes and what the buyer hears. A standing
 * Sale's factura offers Issue again, Reissue and Reverse the Sale, with "This
 * Sale now has no factura" said plainly and a note that leaving it keeps the
 * Sale in the needs-attention queue; a reversed Sale's shows only "Nothing
 * follows"; a manual Tax Invoice's only "Type it again by hand". The cards are
 * `annulment.follow_ups`, decided by the API's own gates: at mismatch Issue
 * again and Reissue are held with the refusal their press would answer, and on
 * a check still checking or unconfirmed their confirms say the SRI has not
 * confirmed the annulment. The presses are the existing Issue again and Reissue forms, drawn
 * inline, and Reverse the Sale links to the Sale's Operator Reversal.
 *
 * THE PORTAL RUNBOOK (#752) sits beside the record form: the portal steps as
 * a numbered checklist, a link to SRI en linea and the menu path for
 * Produccion or Pruebas by the factura's environment, and every field the
 * portal's form asks for with a copy button, the receptor ID drawn with its
 * spaces visible. Every field is `annulability.portal`'s, copied exactly as
 * the API sent it. The ticks are the checklist's own state: the record form
 * never reads them, so recording an annulment made earlier is one step. Below
 * the form, a Sale Invoice's panel says the buyer is sent an Annulment Notice,
 * and every admitted factura's says to take the period's archive again if it
 * already went to the accountant.
 *
 * REINSTATE (#749) sits on the annulled face once the window has closed with
 * the check at mismatch, as `annulment.reinstate` admits it: the confirm by
 * the typed number with an optional note, saying what it may owe and what the
 * buyer hears. At mismatch inside the window one line says it opens once the
 * window closes. A reinstated factura's past annulments - who annulled and
 * reinstated, when, the notes, where the check stood, both buyer notices -
 * are drawn as a trail on whichever face the panel then shows.
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
    return <AnnulledPanel invoice={invoice} onUpdated={onUpdated} />;
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
      <CardContent className="space-y-3 text-sm text-muted-foreground">
        <p>{sentence}</p>
        <PastAnnulments invoice={invoice} />
      </CardContent>
    </Card>
  );
}

function AnnulledPanel({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const current = invoice.annulment?.current ?? null;
  const check = invoice.annulment?.check ?? null;
  // A factura annulled before the annulments ledger existed (#477) has no
  // record there; its trail is the row's own pair.
  const by = current?.annulled_by ?? invoice.annulled_by;
  const at = current?.annulled_at ?? invoice.annulled_at;
  return (
    <Card className={cn("border-l-4", check?.verdict === "mismatch" ? "border-l-destructive" : "border-l-neutral-500")}>
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle>{t("invoicingAnnulPanelAnnulledTitle")}</CardTitle>
          {check ? (
            <Badge variant={ANNULMENT_CHECK_BADGE[check.verdict]}>{t(ANNULMENT_CHECK_BADGE_KEYS[check.verdict])}</Badge>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-2 text-sm text-muted-foreground">
        {check && at ? (
          <AnnulmentCheckSection invoice={invoice} check={check} annulledAt={at} locale={locale} onUpdated={onUpdated} />
        ) : null}
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
        {current?.notice ? <NoticeLine notice={current.notice} locale={locale} /> : null}
        <ReinstateSection invoice={invoice} onUpdated={onUpdated} />
        <FollowUpChooserSection invoice={invoice} />
        <PastAnnulments invoice={invoice} />
      </CardContent>
    </Card>
  );
}

/** The trail's buyer-notice line (#748): held, being sent, sent at, or none owed. */
function NoticeLine({ notice, locale }: { notice: OperatorAnnulmentNotice; locale: AppLocale }) {
  const t = useTranslations("operator");
  const line = annulmentNoticeLine(notice);
  return (
    <p>
      {t(line.key, {
        when: line.sentAt ? (formatDateTime(line.sentAt, PLATFORM_TIME_ZONE, locale) ?? line.sentAt) : "",
      })}
    </p>
  );
}

/**
 * Reinstate on the annulled face (#749): the confirm where the API admits it,
 * or at mismatch inside the window the line saying when it opens.
 */
function ReinstateSection({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const offer = invoice.annulment ? reinstateOffer(invoice.annulment) : null;
  if (offer === null || invoice.number === null) {
    return null;
  }
  if (offer === "after_window") {
    const deadline = invoice.annulability?.window?.deadline;
    return deadline ? (
      <p>{t("invoicingReinstateAfterWindow", { deadline: formatCalendarDay(deadline, locale) })}</p>
    ) : null;
  }
  return (
    <section className="border-t pt-4">
      <OperatorInvoiceReinstate invoiceId={invoice.id} number={invoice.number} onReinstated={onUpdated} />
    </section>
  );
}

/**
 * The trail of past annulments (#749, story 88): each annulment a Reinstate
 * closed, kept as history with its check's last word and both buyer notices.
 */
function PastAnnulments({ invoice, className }: { invoice: OperatorInvoiceDetail; className?: string }) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const lines = pastAnnulmentLines(invoice.annulment?.past ?? []);
  if (lines.length === 0) {
    return null;
  }
  const when = (iso: string | null) => (iso ? (formatDateTime(iso, PLATFORM_TIME_ZONE, locale) ?? iso) : "");
  return (
    <section className={cn("space-y-2 border-t pt-4 text-sm text-muted-foreground", className)}>
      <h3 className="text-base font-semibold text-foreground">{t("invoicingAnnulPanelPastTitle")}</h3>
      <ul className="space-y-3">
        {lines.map((line) => (
          <li key={line.annulledAt} className="space-y-1">
            <p className="text-foreground">
              {t("invoicingAnnulPanelPastTrail", {
                by: line.annulledBy,
                when: when(line.annulledAt),
                reinstatedBy: line.reinstatedBy ?? "",
                reinstatedWhen: when(line.reinstatedAt),
              })}
            </p>
            {line.verdict ? (
              <p>
                {t("invoicingAnnulPanelPastCheck", {
                  verdict: t(ANNULMENT_CHECK_BADGE_KEYS[line.verdict]),
                  count: line.asks,
                })}
              </p>
            ) : null}
            {line.note ? (
              <p>
                <span className="font-medium text-foreground">{t("invoicingAnnulPanelPastNote")}</span>{" "}
                <span className="whitespace-pre-wrap">{line.note}</span>
              </p>
            ) : null}
            {line.reinstateNote ? (
              <p>
                <span className="font-medium text-foreground">{t("invoicingAnnulPanelPastReinstateNote")}</span>{" "}
                <span className="whitespace-pre-wrap">{line.reinstateNote}</span>
              </p>
            ) : null}
            <p>{t(line.notice.key, { when: when(line.notice.sentAt) })}</p>
            {line.reinstatementNotice ? (
              <p>{t(line.reinstatementNotice.key, { when: when(line.reinstatementNotice.sentAt) })}</p>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function FollowUpChooserSection({ invoice }: { invoice: OperatorInvoiceDetail }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const chooser = invoice.annulment ? followUpChooser(invoice.annulment) : null;
  if (chooser === null) {
    return null;
  }
  const { notConfirmed } = chooser;
  function press(kind: FollowUpKind) {
    switch (kind) {
      case "issue_again":
        return <OperatorInvoiceIssueAgain invoice={invoice} inline notConfirmed={notConfirmed} />;
      case "reissue":
        return <OperatorInvoiceReissue invoice={invoice} inline notConfirmed={notConfirmed} />;
      case "reverse_sale":
        return invoice.sale_confirmation_ref ? (
          <Button asChild variant="outline">
            <Link href={`/operator/sales/${encodeURIComponent(invoice.sale_confirmation_ref)}`}>
              {t("invoicingFollowUpReverseLink")}
            </Link>
          </Button>
        ) : null;
      case "nothing_follows":
      case "type_again":
        return null;
    }
  }
  return (
    <section className="space-y-3 border-t pt-4">
      <h3 className="text-base font-semibold text-foreground">{t("invoicingFollowUpTitle")}</h3>
      {chooser.saleHasNoFactura ? (
        <div className="space-y-1">
          <p className="font-medium text-foreground">{t("invoicingFollowUpNoFactura")}</p>
          <p>{t("invoicingFollowUpLeaveIt")}</p>
        </div>
      ) : null}
      {notConfirmed && chooser.cards.some((kind) => kind === "issue_again" || kind === "reissue") ? (
        <p className="text-amber-700">{t("invoicingFollowUpUnconfirmed")}</p>
      ) : null}
      <div className="grid gap-3 md:grid-cols-3">
        {chooser.cards.map((kind) => (
          <div key={kind} className="flex flex-col gap-2 rounded-md border p-3">
            <p className="font-medium text-foreground">{t(FOLLOW_UP_CARD_KEYS[kind].title)}</p>
            <p>{t(FOLLOW_UP_CARD_KEYS[kind].owes)}</p>
            <p>{t(FOLLOW_UP_CARD_KEYS[kind].buyer)}</p>
            <div className="mt-auto pt-1">{press(kind)}</div>
          </div>
        ))}
      </div>
      {chooser.held.map((held) => (
        <p key={held.kind} className="text-destructive">
          {t("invoicingFollowUpHeld", {
            title: t(FOLLOW_UP_CARD_KEYS[held.kind].title),
            reason: apiErrorMessage(errorCopy, new ApiError(held.message, held.code)) ?? held.message,
          })}
        </p>
      ))}
    </section>
  );
}

const ANNULMENT_CHECK_BADGE_KEYS = {
  checking: "invoicingAnnulCheckBadgeChecking",
  confirmed: "invoicingAnnulCheckBadgeConfirmed",
  mismatch: "invoicingAnnulCheckBadgeMismatch",
  unconfirmed: "invoicingAnnulCheckBadgeUnconfirmed",
} as const;

const STATUS_READING_KEYS = {
  annulled: "invoicingAnnulCheckReadingAnnulled",
  authorized: "invoicingAnnulCheckReadingAuthorized",
  none: "invoicingAnnulCheckReadingNone",
} as const;

function AnnulmentCheckSection({
  invoice,
  check,
  annulledAt,
  locale,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  check: OperatorAnnulmentCheck;
  annulledAt: string;
  locale: AppLocale;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const when = (iso: string | null) => (iso ? (formatDateTime(iso, PLATFORM_TIME_ZONE, locale) ?? iso) : "");
  const line = annulmentCheckLine(check, annulledAt);

  async function askAgain() {
    setBusy(true);
    setError(null);
    try {
      onUpdated(await checkOperatorInvoice(invoice.id));
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ?? t("invoicingCheckFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  let text: string;
  switch (line.kind) {
    case "checking":
      text = line.next
        ? t("invoicingAnnulCheckChecking", { since: when(line.since), next: when(line.next) })
        : t("invoicingAnnulCheckCheckingNoNext", { since: when(line.since) });
      break;
    case "confirmed":
      text = t("invoicingAnnulCheckConfirmed", { at: when(line.at) });
      break;
    case "mismatch":
      text = t("invoicingAnnulCheckMismatch", { since: when(line.since) });
      break;
    case "unconfirmed":
      text = line.until
        ? t("invoicingAnnulCheckUnconfirmed", { at: when(line.at), until: when(line.until) })
        : t("invoicingAnnulCheckUnconfirmedEnded", { at: when(line.at) });
      break;
    default:
      text = t("invoicingAnnulCheckUnchecked");
  }

  return (
    <div className="space-y-2">
      <p className={cn(line.kind === "mismatch" ? "font-medium text-destructive" : "text-foreground")}>{text}</p>
      {line.kind === "mismatch" ? <p>{t("invoicingAnnulCheckMismatchHint")}</p> : null}
      {check.answers.length > 0 ? (
        <details className="rounded-md border px-3 py-2">
          <summary className="cursor-pointer text-xs font-medium">
            {t("invoicingAnnulCheckAnswers", { count: check.answers.length })}
          </summary>
          <ul className="mt-2 space-y-1 text-xs">
            {check.answers.map((answer) => (
              <li key={answer.attempt_id}>
                <span className="whitespace-nowrap">{when(answer.started_at)}</span>{" "}
                <span className="font-mono">{answer.service}</span>{" "}
                <span className="font-medium text-foreground">{t(STATUS_READING_KEYS[answer.reading])}</span>
                {answer.reading === "none"
                  ? [
                      ...answer.messages.map((message) => `${message.identifier} ${message.message}`),
                      ...(answer.error ? [answer.error] : []),
                    ].map((raw, index) => (
                      <span key={index} className="block pl-4 font-mono text-muted-foreground">
                        {raw}
                      </span>
                    ))
                  : null}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
      {annulmentCheckOffersCheckStatus(check) ? (
        <div className="flex flex-wrap items-center gap-3 rounded-md bg-muted/40 px-3 py-2">
          <p className="flex-1 text-xs">{t("invoicingAnnulCheckAskHint")}</p>
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void askAgain()}>
            {busy ? t("invoicingCheckingStatus") : t("invoicingCheckStatus")}
          </Button>
        </div>
      ) : null}
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
    </div>
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
  const portal = invoice.annulability?.portal ?? null;
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
        <div className={cn("grid gap-6", portal !== null && "md:grid-cols-2")}>
          {portal !== null ? <PortalChecklist portal={portal} /> : null}
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
            {annulmentSendsNotice(invoice.kind) ? (
              <p className="text-xs text-muted-foreground">{t("invoicingAnnulPanelNotice")}</p>
            ) : null}
            <p className="text-xs text-muted-foreground">{t("invoicingAnnulPanelArchiveAgain")}</p>
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
        </div>
        <PastAnnulments invoice={invoice} className="mt-6" />
      </CardContent>
    </Card>
  );
}

const STEP_TITLE_KEYS = {
  sign_in: "invoicingAnnulPortalStepSignIn",
  open_form: "invoicingAnnulPortalStepOpenForm",
  fill: "invoicingAnnulPortalStepFill",
  accept: "invoicingAnnulPortalStepAccept",
} as const;

const FIELD_LABEL_KEYS = {
  access_key: "invoicingAnnulPortalFieldAccessKey",
  authorization_day: "invoicingAnnulPortalFieldAuthorizationDay",
  receptor_id: "invoicingAnnulPortalFieldReceptorId",
  receptor_email: "invoicingAnnulPortalFieldReceptorEmail",
} as const;

/**
 * The portal steps as a numbered checklist. Its ticks live here and nowhere
 * else: nothing outside this component reads them, so the record form beside
 * it submits the same whether every step is ticked or none is.
 */
function PortalChecklist({ portal }: { portal: OperatorAnnulmentPortal }) {
  const t = useTranslations("operator");
  const [done, setDone] = useState<Partial<Record<AnnulmentPortalStep, boolean>>>({});
  const fields = annulmentPortalFields(portal);

  function stepBody(step: AnnulmentPortalStep) {
    switch (step) {
      case "sign_in":
        return (
          <a
            href={SRI_EN_LINEA_URL}
            target="_blank"
            rel="noreferrer"
            className="text-xs underline underline-offset-4"
          >
            {t("invoicingAnnulPortalOpen")}
          </a>
        );
      case "open_form":
        return <p className="text-xs text-muted-foreground">{t(annulmentPortalPathKey(portal.environment))}</p>;
      case "fill":
        return (
          <div className="space-y-1.5">
            {fields.map((field) => (
              <PortalFieldRow key={field.field} field={field} />
            ))}
          </div>
        );
      case "accept":
        return null;
    }
  }

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <p className="text-sm font-medium">{t("invoicingAnnulPortalTitle")}</p>
        <p className="text-xs text-muted-foreground">{t("invoicingAnnulPortalHint")}</p>
      </div>
      <ol className="space-y-4">
        {ANNULMENT_PORTAL_STEPS.map((step, index) => {
          const ticked = done[step] === true;
          return (
            <li key={step} className="flex gap-3">
              <button
                type="button"
                aria-pressed={ticked}
                aria-label={t("invoicingAnnulPortalStepNumber", { step: index + 1 })}
                onClick={() => setDone((current) => ({ ...current, [step]: !ticked }))}
                className={cn(
                  "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-medium",
                  ticked
                    ? "border-emerald-600 bg-emerald-600 text-white"
                    : "border-muted-foreground/40 text-muted-foreground",
                )}
              >
                {ticked ? "✓" : index + 1}
              </button>
              <div className="min-w-0 flex-1 space-y-1">
                <p className={cn("text-sm font-medium", ticked && "text-muted-foreground line-through")}>
                  {t(STEP_TITLE_KEYS[step])}
                </p>
                {stepBody(step)}
              </div>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/** One field of the portal's form, drawn as the API sent it, with its copy button. */
function PortalFieldRow({ field }: { field: AnnulmentPortalField }) {
  const t = useTranslations("operator");
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(field.value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access can be refused (insecure origin, denied permission).
      toast.error(t("invoicingAnnulPortalCopyFailed"));
    }
  }

  return (
    <div className="flex items-center justify-between gap-2 rounded-md border bg-background px-2 py-1">
      <div className="min-w-0">
        <p className="text-[11px] text-muted-foreground">{t(FIELD_LABEL_KEYS[field.field])}</p>
        <p className="break-all font-mono text-xs">
          {field.showWhitespace
            ? visibleWhitespace(field.value).map((segment, index) =>
                segment.space ? (
                  <span
                    key={index}
                    title={t("invoicingAnnulPortalSpace")}
                    aria-label={t("invoicingAnnulPortalSpace")}
                    className="mx-px rounded-sm bg-amber-100 px-0.5 text-amber-700"
                  >
                    ␣
                  </span>
                ) : (
                  <span key={index}>{segment.text}</span>
                ),
              )
            : field.value}
        </p>
      </div>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 shrink-0 px-2 text-xs"
        onClick={() => void copy()}
      >
        {copied ? t("invoicingAnnulPortalCopied") : t("invoicingAnnulPortalCopy")}
      </Button>
    </div>
  );
}
