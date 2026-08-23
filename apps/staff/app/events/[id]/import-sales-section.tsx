"use client";

import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
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
  Label,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, fetchEventsJSON, type TicketType } from "@/lib/events-api";
import {
  PLATFORM_TIME_ZONE,
  formatCalendarDay,
  formatDateTime,
  formatMoney,
  formatNumber,
} from "@/lib/format";
import {
  commitSaleImport,
  previewSaleImport,
  undoSaleImport,
  type ImportCommitResult,
  type ImportHistoryEntry,
  type ImportPreviewResult,
} from "@/lib/imports-api";

import { RecordSaleDialog } from "./record-sale-dialog";
import { useSalesRefreshNotify } from "./sales-refresh";

type ImportSalesSectionProps = {
  eventId: string;
  /**
   * The Event's own timezone, which the import history's timestamps are drawn
   * in. Null on an Event that names none, and then the platform's clock beneath
   * it — never the reader's laptop, which is what an unqualified `Intl` call
   * silently used before #289.
   */
  timezone: string | null;
};

/**
 * The one place an Organization records sales, by either of two routes: upload
 * a filled-in Sale Import file, or record one sale by hand (#369, ADR 0052).
 *
 * GATE. The page renders this section for an Org Admin OR an Event Owner, while
 * every endpoint behind it — the Sale Import's and the manual record's alike —
 * admits only whoever may manage the Event's sales, which today is the Org
 * Admin. So an Event Owner is shown the tool and refused by it. That mismatch
 * PREDATES this section growing a second route and is deliberately left alone
 * here (#369, out of scope): the record-a-sale modal mirrors the section's gate
 * exactly rather than widening or narrowing it, so the two routes are wrong in
 * the same way and are fixed in one move when the gate is.
 */
export function ImportSalesSection({ eventId, timezone }: ImportSalesSectionProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [ticketTypes, setTicketTypes] = useState<TicketType[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [idempotencyKey, setIdempotencyKey] = useState("");
  const [preview, setPreview] = useState<ImportPreviewResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [raisingTypeId, setRaisingTypeId] = useState<string | null>(null);
  const [skipRows, setSkipRows] = useState<Set<number>>(new Set());
  const [committed, setCommitted] = useState<ImportCommitResult | null>(null);
  const [history, setHistory] = useState<ImportHistoryEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [undoTarget, setUndoTarget] = useState<ImportHistoryEntry | null>(null);
  const [notifyBuyers, setNotifyBuyers] = useState(false);
  const [undoing, setUndoing] = useState(false);
  // The other route to the same act: one sale typed instead of uploaded.
  const [recordOpen, setRecordOpen] = useState(false);

  // Signals the sibling Sales list to re-fetch its current view after a
  // successful commit/undo (paired with the existing success toast).
  const notifySalesRefresh = useSalesRefreshNotify();

  const currency = ticketTypes[0]?.currency ?? "USD";
  const zone = timezone ?? PLATFORM_TIME_ZONE;

  /**
   * The sentence for a failed request, in this reader's language.
   *
   * The catalog by the API's error code first — IMPORT_NOT_LATEST_BATCH,
   * IMPORT_ALREADY_REVERSED and the rest are all keyed, because they are the
   * refusals this surface deliberately shows — then the API's own English for a
   * code the catalog has not heard of, then this surface's own sentence for a
   * request that never reached the API at all (ADR 0023).
   */
  const failureMessage = useCallback(
    (failure: unknown, fallback: string) =>
      (failure instanceof ApiError ? apiErrorMessage(errorCopy, failure) : null) ?? fallback,
    [errorCopy],
  );

  const loadTicketTypes = useCallback(async () => {
    try {
      const types = await fetchEventsJSON<TicketType[]>(`/api/events/${eventId}/ticket-types`);
      setTicketTypes(types);
    } catch {
      // Non-fatal: the raise-capacity action degrades if types can't be loaded.
    }
  }, [eventId]);

  const loadHistory = useCallback(async () => {
    try {
      const entries = await fetchEventsJSON<ImportHistoryEntry[]>(`/api/events/${eventId}/sale-imports`);
      setHistory(entries);
    } catch {
      // History is a read-only convenience; ignore load failures.
    }
  }, [eventId]);

  useEffect(() => {
    void loadTicketTypes();
    void loadHistory();
  }, [loadTicketTypes, loadHistory]);

  function resetForNewFile(next: File | null) {
    setFile(next);
    setPreview(null);
    setCommitted(null);
    setSkipRows(new Set());
    setError(null);
    setIdempotencyKey(next ? crypto.randomUUID() : "");
  }

  const runPreview = useCallback(
    async (target: File) => {
      setPreviewing(true);
      setError(null);
      try {
        const result = await previewSaleImport(eventId, target);
        setPreview(result);
        // Drop any skip decisions that no longer point at a duplicate row.
        setSkipRows((current) => {
          const dupRows = new Set(result.rows.filter((r) => r.possible_duplicate).map((r) => r.row));
          return new Set([...current].filter((row) => dupRows.has(row)));
        });
      } catch (previewError) {
        setPreview(null);
        setError(failureMessage(previewError, t("importPreviewFailed")));
      } finally {
        setPreviewing(false);
      }
    },
    [eventId, failureMessage, t],
  );

  async function handlePreview() {
    if (!file) {
      toast.error(t("importChooseFileFirst"));
      return;
    }
    await runPreview(file);
  }

  function toggleSkip(row: number) {
    setSkipRows((current) => {
      const next = new Set(current);
      if (next.has(row)) {
        next.delete(row);
      } else {
        next.add(row);
      }
      return next;
    });
  }

  async function raiseCapacity(ticketTypeId: string, newCapacity: number) {
    const ticketType = ticketTypes.find((type) => type.id === ticketTypeId);
    if (!ticketType) {
      toast.error(t("importTicketTypeMissing"));
      return;
    }
    setRaisingTypeId(ticketTypeId);
    try {
      await fetchEventsJSON<TicketType>(`/api/events/${eventId}/ticket-types/${ticketTypeId}`, {
        method: "PATCH",
        body: JSON.stringify({
          name: ticketType.name,
          description: ticketType.description,
          price_cents: ticketType.price_cents,
          capacity: newCapacity,
          sort_order: ticketType.sort_order,
        }),
      });
      // The Ticket Type's name is data and goes in as it was coined.
      toast.success(
        t("importRaised", { name: ticketType.name, capacity: formatNumber(newCapacity, locale) }),
      );
      await loadTicketTypes();
      if (file) {
        await runPreview(file);
      }
    } catch (raiseError) {
      toast.error(failureMessage(raiseError, t("importRaiseFailed")));
    } finally {
      setRaisingTypeId(null);
    }
  }

  async function handleCommit() {
    if (!file || !preview || !preview.committable) {
      return;
    }
    setCommitting(true);
    setError(null);
    try {
      const result = await commitSaleImport(eventId, file, idempotencyKey, [...skipRows]);
      setCommitted(result);
      toast.success(
        result.replayed
          ? t("importReplayed")
          : t("importSucceeded", { count: result.sale_count }),
      );
      await Promise.all([loadTicketTypes(), loadHistory()]);
      // Newly imported sales are now visible: refresh the Sales list's current view.
      notifySalesRefresh();
    } catch (commitError) {
      const message = failureMessage(commitError, t("importFailed"));
      setError(message);
      if (commitError instanceof ApiError && commitError.code === "IMPORT_BATCH_FAILED") {
        // A capacity race was lost since preview: re-preview to show the block.
        // The toast says what to DO about it, which the code's own catalogued
        // sentence in the alert above does not.
        toast.error(t("importCapacityRace"));
        if (file) {
          await runPreview(file);
        }
      } else {
        toast.error(message);
      }
    } finally {
      setCommitting(false);
    }
  }

  /**
   * A hand-typed sale is real the moment it is saved, so the screen behind the
   * modal has to agree with it: the Sales list re-reads its current view, the
   * Ticket Types re-read the capacity it just took, and a preview of a file
   * still on screen is re-run over that new capacity rather than left claiming
   * room it no longer has. It runs after EVERY sale of a keep-adding sitting,
   * so what is behind the modal is right the moment it is closed.
   *
   * Whether that save closes the modal is the modal's own call — Keep adding
   * decides it (#372) — so this does not close it.
   */
  async function handleRecorded() {
    notifySalesRefresh();
    await loadTicketTypes();
    if (file) {
      await runPreview(file);
    }
  }

  function openUndo(entry: ImportHistoryEntry) {
    setUndoTarget(entry);
    setNotifyBuyers(false);
  }

  async function confirmUndo() {
    if (!undoTarget) {
      return;
    }
    setUndoing(true);
    try {
      const result = await undoSaleImport(eventId, undoTarget.batch_id, notifyBuyers);
      // Two whole sentences, not one with " and notified buyers" appended: the
      // clause does not sit at the end in every language, and a plural has to
      // agree inside each of them.
      toast.success(
        result.notified
          ? t("importUndoneNotified", { count: result.sale_count })
          : t("importUndone", { count: result.sale_count }),
      );
      setUndoTarget(null);
      await Promise.all([loadTicketTypes(), loadHistory()]);
      // Reversed sales now leave the default active view: refresh the Sales list.
      notifySalesRefresh();
    } catch (undoError) {
      toast.error(failureMessage(undoError, t("importUndoFailed")));
    } finally {
      setUndoing(false);
    }
  }

  // Only the most recent batch is reversible; identify it once for the history UI.
  const latestBatchId = history[0]?.batch_id;

  const skippedCount = preview ? preview.rows.filter((r) => skipRows.has(r.row)).length : 0;
  const commitCount = preview ? preview.total_rows - skippedCount : 0;
  const invalidCount = preview ? preview.total_rows - preview.valid_rows : 0;
  const oversoldImpacts = preview ? preview.capacity_impact.filter((c) => c.oversold) : [];

  return (
    <Card id="import-sales" className="scroll-mt-6">
      <CardHeader>
        <CardTitle>{t("importTitle")}</CardTitle>
        <CardDescription>{t("importDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Route 1 — type one sale. First because it is the shorter road, and
            the one an organizer with a single cash payment should not have to
            read past a spreadsheet to find. */}
        <div className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1 text-sm">
            <p className="font-medium">{t("recordManualHeading")}</p>
            <p className="text-muted-foreground">{t("recordManualHint")}</p>
          </div>
          <Button type="button" size="sm" onClick={() => setRecordOpen(true)}>
            {t("recordManualOpen")}
          </Button>
        </div>

        {/* Route 2 — upload a file. The four steps below are all its own. */}
        <div className="space-y-1 border-t pt-6">
          <p className="text-sm font-medium">{t("importFileHeading")}</p>
          <p className="text-sm text-muted-foreground">{t("importFileHint")}</p>
        </div>

        {/* Step 1 — download template */}
        <div className="space-y-2">
          <p className="text-sm font-medium">{t("importStepTemplate")}</p>
          <p className="text-sm text-muted-foreground">{t("importTemplateHint")}</p>
          <Button asChild variant="outline" size="sm">
            <a href={`/api/events/${eventId}/sale-imports/template`}>{t("importTemplateDownload")}</a>
          </Button>
        </div>

        {/* Step 2 — upload + preview */}
        <div className="space-y-2">
          <p className="text-sm font-medium">{t("importStepUpload")}</p>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <input
              type="file"
              accept=".csv,.xlsx"
              className="text-sm file:mr-3 file:rounded-md file:border file:border-input file:bg-background file:px-3 file:py-1.5 file:text-sm"
              onChange={(event) => resetForNewFile(event.target.files?.[0] ?? null)}
            />
            <Button type="button" size="sm" disabled={!file || previewing} onClick={() => void handlePreview()}>
              {previewing ? t("importChecking") : t("importPreview")}
            </Button>
          </div>
          {/* A filename is the reader's own and is never translated. */}
          {file ? (
            <p className="text-sm text-muted-foreground">{t("importSelectedFile", { name: file.name })}</p>
          ) : null}
        </div>

        {error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("importProblemTitle")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {/* Step 3 — preview verdicts */}
        {preview ? (
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">{t("importStepReview")}</p>
              <p className="text-sm text-muted-foreground">
                {t("importRowsValid", {
                  valid: formatNumber(preview.valid_rows, locale),
                  total: formatNumber(preview.total_rows, locale),
                })}
              </p>
            </div>

            {invalidCount > 0 ? (
              <Alert variant="destructive">
                <AlertTitle>{t("importInvalidTitle", { count: invalidCount })}</AlertTitle>
                <AlertDescription>{t("importInvalidBody")}</AlertDescription>
              </Alert>
            ) : null}

            {/* Oversell block + inline raise-capacity */}
            {oversoldImpacts.length > 0 ? (
              <Alert variant="destructive">
                <AlertTitle>{t("importOversellTitle")}</AlertTitle>
                <AlertDescription>
                  <div className="space-y-3">
                    <p>{t("importOversellBody")}</p>
                    {oversoldImpacts.map((impact) => {
                      const target = impact.sold_count + impact.requested;
                      return (
                        <div
                          key={impact.ticket_type_id}
                          className="flex flex-col gap-2 rounded-md border border-destructive/40 p-3 sm:flex-row sm:items-center sm:justify-between"
                        >
                          <span className="text-sm">
                            {/* Four numbers and a name in one sentence: exactly
                                the case where concatenating in JSX would fix the
                                English word order into the Spanish. */}
                            {t.rich("importOversellLine", {
                              name: (chunks) => <strong>{chunks}</strong>,
                              ticketType: impact.ticket_type_name,
                              sold: formatNumber(impact.sold_count, locale),
                              requested: formatNumber(impact.requested, locale),
                              capacity: formatNumber(impact.capacity, locale),
                              overage: formatNumber(impact.overage, locale),
                            })}
                          </span>
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={raisingTypeId === impact.ticket_type_id}
                            onClick={() => void raiseCapacity(impact.ticket_type_id, target)}
                          >
                            {raisingTypeId === impact.ticket_type_id
                              ? t("importRaising")
                              : t("importRaiseCapacity", { capacity: formatNumber(target, locale) })}
                          </Button>
                        </div>
                      );
                    })}
                  </div>
                </AlertDescription>
              </Alert>
            ) : null}

            {/* Capacity impact per Ticket Type */}
            {preview.capacity_impact.length > 0 ? (
              <div className="space-y-2">
                <p className="text-sm font-medium">{t("importCapacityImpact")}</p>
                <div className="space-y-2">
                  {preview.capacity_impact.map((impact) => (
                    <div
                      key={impact.ticket_type_id}
                      className="flex items-center justify-between rounded-md border p-3 text-sm"
                    >
                      <span>{impact.ticket_type_name}</span>
                      <span className={impact.oversold ? "text-destructive" : "text-muted-foreground"}>
                        {impact.oversold
                          ? t("importCapacityOver", {
                              after: formatNumber(impact.sold_count + impact.requested, locale),
                              capacity: formatNumber(impact.capacity, locale),
                              overage: formatNumber(impact.overage, locale),
                            })
                          : t("importCapacityAfter", {
                              after: formatNumber(impact.sold_count + impact.requested, locale),
                              capacity: formatNumber(impact.capacity, locale),
                            })}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            ) : null}

            {/* Per-row verdicts */}
            <div className="space-y-2">
              <p className="text-sm font-medium">{t("importRowsHeading")}</p>
              <div className="space-y-2">
                {preview.rows.map((row) => {
                  const skipped = skipRows.has(row.row);
                  return (
                    <div
                      key={row.row}
                      className={`rounded-md border p-3 text-sm ${
                        !row.valid ? "border-destructive/50" : skipped ? "opacity-60" : ""
                      }`}
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">
                            {t("importRowNumber", { row: formatNumber(row.row, locale) })}
                          </span>
                          {/* A Customer's name and email are data. */}
                          <span className="text-muted-foreground">
                            {`${row.customer_first_name} ${row.customer_last_name}`.trim() || "—"} · {row.customer_email || "—"}
                          </span>
                          {row.ticket_type_name ? (
                            <Badge variant="secondary">{row.ticket_type_name}</Badge>
                          ) : row.ticket_type ? (
                            <Badge variant="secondary">{row.ticket_type}</Badge>
                          ) : null}
                          {row.quantity ? (
                            <span className="text-muted-foreground">
                              ×{formatNumber(row.quantity, locale)}
                            </span>
                          ) : null}
                          {typeof row.amount_cents === "number" ? (
                            <span className="text-muted-foreground">
                              @ {formatMoney(row.amount_cents, currency, locale)}
                            </span>
                          ) : null}
                          {row.valid ? (
                            <Badge variant="success">{t("importValid")}</Badge>
                          ) : (
                            <Badge variant="destructive">{t("importInvalid")}</Badge>
                          )}
                          {row.possible_duplicate ? (
                            <Badge variant="warning">{t("importPossibleDuplicate")}</Badge>
                          ) : null}
                        </div>
                        {row.possible_duplicate ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            onClick={() => toggleSkip(row.row)}
                          >
                            {skipped ? t("importKeep") : t("importSkip")}
                          </Button>
                        ) : null}
                      </div>
                      {/* A row-level validation failure carries no error CODE
                          from the API — only a column name and an English
                          sentence — so this is the one place on these surfaces
                          where ADR 0023's English floor is the whole answer. The
                          column name is the heading printed in the template the
                          organizer filled in, so it is left exactly as the file
                          spells it. */}
                      {row.errors && row.errors.length > 0 ? (
                        <ul className="mt-2 list-disc space-y-1 pl-5 text-destructive">
                          {row.errors.map((rowError, index) => (
                            <li key={`${row.row}-${rowError.field}-${index}`}>
                              {t("importRowError", {
                                field: rowError.field,
                                message: rowError.message,
                              })}
                            </li>
                          ))}
                        </ul>
                      ) : null}
                      {row.possible_duplicate && !skipped ? (
                        <p className="mt-2 text-xs text-muted-foreground">
                          {/* A calendar day, not a moment: it is drawn with
                              formatCalendarDay so it is never re-read as UTC
                              midnight and shown as the day before. */}
                          {t("importDuplicateHint", {
                            date: row.duplicate_of_date
                              ? formatCalendarDay(row.duplicate_of_date, locale)
                              : "—",
                          })}
                        </p>
                      ) : null}
                      {skipped ? (
                        <p className="mt-2 text-xs text-muted-foreground">{t("importSkipped")}</p>
                      ) : null}
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Step 4 — confirm */}
            <div className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm">
                <p className="font-medium">{t("importStepConfirm")}</p>
                <p className="text-muted-foreground">
                  {skippedCount > 0
                    ? t("importConfirmSummarySkipped", {
                        count: commitCount,
                        skipped: formatNumber(skippedCount, locale),
                      })
                    : t("importConfirmSummary", { count: commitCount })}
                </p>
              </div>
              <Button
                type="button"
                disabled={!preview.committable || committing || commitCount === 0}
                onClick={() => void handleCommit()}
              >
                {committing ? t("importCommitting") : t("importCommit", { count: commitCount })}
              </Button>
            </div>
            {!preview.committable ? (
              <p className="text-sm text-muted-foreground">{t("importBlocked")}</p>
            ) : null}
          </div>
        ) : null}

        {committed ? (
          <Alert>
            <AlertTitle>{t("importCompleteTitle")}</AlertTitle>
            <AlertDescription>
              {t("importCompleteBody", {
                count: committed.sale_count,
                batch: committed.batch_id,
              })}
            </AlertDescription>
          </Alert>
        ) : null}

        {/* Import history */}
        <div className="space-y-2 border-t pt-4">
          <p className="text-sm font-medium">{t("importHistoryHeading")}</p>
          {history.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("importHistoryEmpty")}</p>
          ) : (
            <div className="space-y-2">
              {history.map((entry) => {
                // Undo is offered only on the latest batch, and only while it is
                // still committed (an already-reversed batch shows its status).
                const undoable = entry.batch_id === latestBatchId && entry.status === "committed";
                return (
                  <div
                    key={entry.batch_id}
                    className="flex flex-col gap-2 rounded-md border p-3 text-sm sm:flex-row sm:items-center sm:justify-between"
                  >
                    <span>{formatDateTime(entry.created_at, zone, locale)}</span>
                    <div className="flex items-center gap-3">
                      <span className="text-muted-foreground">
                        {t("importHistorySales", { count: entry.sale_count })}
                        {/* An email address is data; the interpuncts between
                            these three independent facts are punctuation, not
                            copy, and read the same in both languages. */}
                        {entry.actor_email ? ` · ${entry.actor_email}` : ""}
                        {entry.status !== "committed"
                          ? ` · ${entry.status === "reversed" ? t("importStatusReversed") : entry.status}`
                          : ""}
                      </span>
                      {undoable ? (
                        <Button type="button" size="sm" variant="outline" onClick={() => openUndo(entry)}>
                          {t("importUndo")}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </CardContent>

      {/* The modal is rendered from inside this section on purpose: it inherits
          the section's gate rather than restating one (see the note above). */}
      <RecordSaleDialog
        eventId={eventId}
        open={recordOpen}
        ticketTypes={ticketTypes}
        currency={currency}
        timezone={timezone}
        onClose={() => setRecordOpen(false)}
        onRecorded={() => void handleRecorded()}
      />

      <Dialog open={undoTarget !== null} onOpenChange={(open) => (!open ? setUndoTarget(null) : undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("importUndoTitle")}</DialogTitle>
            <DialogDescription>
              {undoTarget
                ? t("importUndoBody", { count: undoTarget.sale_count })
                : t("importUndoBodyUnknown")}
            </DialogDescription>
          </DialogHeader>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={notifyBuyers}
              onChange={(event) => setNotifyBuyers(event.target.checked)}
            />
            <span>
              <Label className="font-medium">{t("importNotifyBuyers")}</Label>
              <span className="block text-muted-foreground">{t("importNotifyHint")}</span>
            </span>
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={undoing} onClick={() => setUndoTarget(null)}>
              {t("importUndoCancel")}
            </Button>
            <Button type="button" disabled={undoing} onClick={() => void confirmUndo()}>
              {undoing ? t("importUndoing") : t("importUndoConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
