"use client";

import { useCallback, useEffect, useState } from "react";

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

import { ApiError, fetchEventsJSON, formatPriceCents, type TicketType } from "@/lib/events-api";
import {
  commitSaleImport,
  formatBatchTimestamp,
  previewSaleImport,
  undoSaleImport,
  type ImportCommitResult,
  type ImportHistoryEntry,
  type ImportPreviewResult,
} from "@/lib/imports-api";

import { useSalesRefreshNotify } from "./sales-refresh";

type ImportSalesSectionProps = {
  eventId: string;
};

export function ImportSalesSection({ eventId }: ImportSalesSectionProps) {
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

  // Signals the sibling Sales list to re-fetch its current view after a
  // successful commit/undo (paired with the existing success toast).
  const notifySalesRefresh = useSalesRefreshNotify();

  const currency = ticketTypes[0]?.currency ?? "USD";

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
        setError(previewError instanceof Error ? previewError.message : "Failed to preview file");
      } finally {
        setPreviewing(false);
      }
    },
    [eventId],
  );

  async function handlePreview() {
    if (!file) {
      toast.error("Choose a .csv or .xlsx file first");
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
    const ticketType = ticketTypes.find((t) => t.id === ticketTypeId);
    if (!ticketType) {
      toast.error("Ticket type not found; reload and try again");
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
      toast.success(`Raised ${ticketType.name} capacity to ${newCapacity}`);
      await loadTicketTypes();
      if (file) {
        await runPreview(file);
      }
    } catch (raiseError) {
      toast.error(raiseError instanceof Error ? raiseError.message : "Failed to raise capacity");
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
          ? "This batch was already imported"
          : `Imported ${result.sale_count} sale${result.sale_count === 1 ? "" : "s"}`,
      );
      await Promise.all([loadTicketTypes(), loadHistory()]);
      // Newly imported sales are now visible: refresh the Sales list's current view.
      notifySalesRefresh();
    } catch (commitError) {
      const message = commitError instanceof Error ? commitError.message : "Failed to import sales";
      setError(message);
      if (commitError instanceof ApiError && commitError.code === "IMPORT_BATCH_FAILED") {
        // A capacity race was lost since preview: re-preview to show the block.
        toast.error("Capacity changed since preview. Review the updated preview and try again.");
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
      toast.success(
        `Undid ${result.sale_count} sale${result.sale_count === 1 ? "" : "s"}${
          result.notified ? " and notified buyers" : ""
        }`,
      );
      setUndoTarget(null);
      await Promise.all([loadTicketTypes(), loadHistory()]);
      // Reversed sales now leave the default active view: refresh the Sales list.
      notifySalesRefresh();
    } catch (undoError) {
      const message = undoError instanceof Error ? undoError.message : "Failed to undo import";
      toast.error(message);
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
        <CardTitle>Import sales</CardTitle>
        <CardDescription>
          Record off-platform (cash or bank transfer) sales. Download the template, fill it in, upload the .csv or
          .xlsx, review the preview, then confirm.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Step 1 — download template */}
        <div className="space-y-2">
          <p className="text-sm font-medium">1. Download the template</p>
          <p className="text-sm text-muted-foreground">
            The template pre-lists this Event&apos;s Ticket Types so names and IDs match exactly.
          </p>
          <Button asChild variant="outline" size="sm">
            <a href={`/api/events/${eventId}/sale-imports/template`}>Download .xlsx template</a>
          </Button>
        </div>

        {/* Step 2 — upload + preview */}
        <div className="space-y-2">
          <p className="text-sm font-medium">2. Upload your file</p>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <input
              type="file"
              accept=".csv,.xlsx"
              className="text-sm file:mr-3 file:rounded-md file:border file:border-input file:bg-background file:px-3 file:py-1.5 file:text-sm"
              onChange={(event) => resetForNewFile(event.target.files?.[0] ?? null)}
            />
            <Button type="button" size="sm" disabled={!file || previewing} onClick={() => void handlePreview()}>
              {previewing ? "Checking..." : "Preview"}
            </Button>
          </div>
          {file ? <p className="text-sm text-muted-foreground">Selected: {file.name}</p> : null}
        </div>

        {error ? (
          <Alert variant="destructive">
            <AlertTitle>Import problem</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {/* Step 3 — preview verdicts */}
        {preview ? (
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">3. Review the preview</p>
              <p className="text-sm text-muted-foreground">
                {preview.valid_rows}/{preview.total_rows} rows valid
              </p>
            </div>

            {invalidCount > 0 ? (
              <Alert variant="destructive">
                <AlertTitle>
                  {invalidCount} row{invalidCount === 1 ? "" : "s"} need fixing
                </AlertTitle>
                <AlertDescription>
                  Fix the flagged rows in your file and upload it again. All problems are shown at once below.
                </AlertDescription>
              </Alert>
            ) : null}

            {/* Oversell block + inline raise-capacity */}
            {oversoldImpacts.length > 0 ? (
              <Alert variant="destructive">
                <AlertTitle>Import would oversell</AlertTitle>
                <AlertDescription>
                  <div className="space-y-3">
                    <p>
                      Raise the Ticket Type capacity to fit, or remove rows and re-upload. You can&apos;t commit while
                      any type is oversold.
                    </p>
                    {oversoldImpacts.map((impact) => {
                      const target = impact.sold_count + impact.requested;
                      return (
                        <div
                          key={impact.ticket_type_id}
                          className="flex flex-col gap-2 rounded-md border border-destructive/40 p-3 sm:flex-row sm:items-center sm:justify-between"
                        >
                          <span className="text-sm">
                            <strong>{impact.ticket_type_name}</strong>: {impact.sold_count} sold +{" "}
                            {impact.requested} requested exceeds capacity {impact.capacity} by {impact.overage}.
                          </span>
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={raisingTypeId === impact.ticket_type_id}
                            onClick={() => void raiseCapacity(impact.ticket_type_id, target)}
                          >
                            {raisingTypeId === impact.ticket_type_id
                              ? "Raising..."
                              : `Raise capacity to ${target}`}
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
                <p className="text-sm font-medium">Capacity impact</p>
                <div className="space-y-2">
                  {preview.capacity_impact.map((impact) => (
                    <div
                      key={impact.ticket_type_id}
                      className="flex items-center justify-between rounded-md border p-3 text-sm"
                    >
                      <span>{impact.ticket_type_name}</span>
                      <span className={impact.oversold ? "text-destructive" : "text-muted-foreground"}>
                        {impact.sold_count + impact.requested}/{impact.capacity} after import
                        {impact.oversold ? ` (over by ${impact.overage})` : ""}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            ) : null}

            {/* Per-row verdicts */}
            <div className="space-y-2">
              <p className="text-sm font-medium">Rows</p>
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
                          <span className="font-medium">Row {row.row}</span>
                          <span className="text-muted-foreground">
                            {`${row.customer_first_name} ${row.customer_last_name}`.trim() || "—"} · {row.customer_email || "—"}
                          </span>
                          {row.ticket_type_name ? (
                            <Badge variant="secondary">{row.ticket_type_name}</Badge>
                          ) : row.ticket_type ? (
                            <Badge variant="secondary">{row.ticket_type}</Badge>
                          ) : null}
                          {row.quantity ? <span className="text-muted-foreground">×{row.quantity}</span> : null}
                          {typeof row.amount_cents === "number" ? (
                            <span className="text-muted-foreground">
                              @ {formatPriceCents(row.amount_cents, currency)}
                            </span>
                          ) : null}
                          {row.valid ? (
                            <Badge variant="success">Valid</Badge>
                          ) : (
                            <Badge variant="destructive">Invalid</Badge>
                          )}
                          {row.possible_duplicate ? (
                            <Badge variant="warning">Possible duplicate</Badge>
                          ) : null}
                        </div>
                        {row.possible_duplicate ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            onClick={() => toggleSkip(row.row)}
                          >
                            {skipped ? "Keep" : "Skip"}
                          </Button>
                        ) : null}
                      </div>
                      {row.errors && row.errors.length > 0 ? (
                        <ul className="mt-2 list-disc space-y-1 pl-5 text-destructive">
                          {row.errors.map((rowError, index) => (
                            <li key={`${row.row}-${rowError.field}-${index}`}>
                              {rowError.field}: {rowError.message}
                            </li>
                          ))}
                        </ul>
                      ) : null}
                      {row.possible_duplicate && !skipped ? (
                        <p className="mt-2 text-xs text-muted-foreground">
                          Matches an existing sale on {row.duplicate_of_date}. Kept unless you skip it.
                        </p>
                      ) : null}
                      {skipped ? <p className="mt-2 text-xs text-muted-foreground">Skipped — not imported.</p> : null}
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Step 4 — confirm */}
            <div className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm">
                <p className="font-medium">4. Confirm import</p>
                <p className="text-muted-foreground">
                  {commitCount} sale{commitCount === 1 ? "" : "s"} will be recorded
                  {skippedCount > 0 ? ` (${skippedCount} skipped)` : ""}. Each customer is emailed a confirmation.
                </p>
              </div>
              <Button
                type="button"
                disabled={!preview.committable || committing || commitCount === 0}
                onClick={() => void handleCommit()}
              >
                {committing ? "Importing..." : `Import ${commitCount} sale${commitCount === 1 ? "" : "s"}`}
              </Button>
            </div>
            {!preview.committable ? (
              <p className="text-sm text-muted-foreground">
                Commit is blocked until every row is valid and no Ticket Type is oversold.
              </p>
            ) : null}
          </div>
        ) : null}

        {committed ? (
          <Alert>
            <AlertTitle>Import complete</AlertTitle>
            <AlertDescription>
              Recorded {committed.sale_count} sale{committed.sale_count === 1 ? "" : "s"} (batch {committed.batch_id}).
            </AlertDescription>
          </Alert>
        ) : null}

        {/* Import history */}
        <div className="space-y-2 border-t pt-4">
          <p className="text-sm font-medium">Import history</p>
          {history.length === 0 ? (
            <p className="text-sm text-muted-foreground">No imports yet for this Event.</p>
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
                    <span>{formatBatchTimestamp(entry.created_at)}</span>
                    <div className="flex items-center gap-3">
                      <span className="text-muted-foreground">
                        {entry.sale_count} sale{entry.sale_count === 1 ? "" : "s"}
                        {entry.actor_email ? ` · ${entry.actor_email}` : ""}
                        {entry.status !== "committed" ? ` · ${entry.status}` : ""}
                      </span>
                      {undoable ? (
                        <Button type="button" size="sm" variant="outline" onClick={() => openUndo(entry)}>
                          Undo
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

      <Dialog open={undoTarget !== null} onOpenChange={(open) => (!open ? setUndoTarget(null) : undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Undo this import?</DialogTitle>
            <DialogDescription>
              This reverses{" "}
              {undoTarget ? `${undoTarget.sale_count} sale${undoTarget.sale_count === 1 ? "" : "s"}` : "the batch"} and
              restores capacity. Only the most recent import can be undone.
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
              <Label className="font-medium">Notify buyers</Label>
              <span className="block text-muted-foreground">
                Email each affected buyer that their confirmation is cancelled. Leave unchecked to undo silently.
              </span>
            </span>
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={undoing} onClick={() => setUndoTarget(null)}>
              Cancel
            </Button>
            <Button type="button" disabled={undoing} onClick={() => void confirmUndo()}>
              {undoing ? "Undoing..." : "Undo import"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
