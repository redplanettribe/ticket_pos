"use client";

import { useEffect, useState } from "react";

import {
  Alert,
  AlertDescription,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
} from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { fetchOperatorTaxDocumentArchiveSummary } from "@/lib/operator-api";
import {
  type TaxDocumentArchiveRange,
  type TaxDocumentArchiveSummary,
  isTaxDocumentArchiveRangeComplete,
  isTaxDocumentArchiveRangeInverted,
  startingTaxDocumentArchiveRange,
  taxDocumentArchiveSummaryDisplay,
  taxDocumentArchiveUrl,
} from "@/lib/tax-document-archive";

/** Which range a summary was read for. */
function summaryRangeKey({ from, to }: TaxDocumentArchiveRange): string {
  return `${from}/${to}`;
}

/** The summary's read for the dialog's current range. */
type SummaryState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "loaded"; summary: TaxDocumentArchiveSummary };

/**
 * What the archive of the chosen range will hold (#631): the counts, the
 * warning that unsettled documents are left out, or the empty-period message.
 * Nothing here holds the download back — a failed read says so and leaves the
 * button alone.
 */
function TaxDocumentArchiveSummaryNote({ state }: { state: SummaryState }) {
  const t = useTranslations("operator.taxDocumentArchive");
  if (state.status === "loading") {
    return (
      <p className="text-sm text-muted-foreground" role="status">
        {t("summaryLoading")}
      </p>
    );
  }
  if (state.status === "error") {
    return <p className="text-sm text-destructive">{t("summaryFailed")}</p>;
  }
  const display = taxDocumentArchiveSummaryDisplay(state.summary);
  const unsettled = display.kind === "counts" ? 0 : display.unsettled;
  return (
    <div className="flex flex-col gap-2" role="status">
      <p className="text-sm">
        {display.kind === "empty"
          ? t("summaryEmpty")
          : t("summaryCounts", { facturas: display.facturas, creditNotes: display.creditNotes })}
      </p>
      {unsettled > 0 ? (
        <Alert variant="warning">
          <AlertDescription>{t("summaryUnsettled", { count: unsettled })}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}

type TaxDocumentArchiveDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The list's Emission Date range, "" for an open end, as the address bar holds it. */
  listRange: { issuedFrom: string; issuedTo: string };
};

/**
 * The Tax Document Archive's dialog (#630, spec #629): a From and a To
 * Emission Date, a note that the list's other filters don't apply, what the
 * archive of that range will hold (#631), and the download.
 *
 * THE RANGE IS SEEDED EACH TIME THE DIALOG OPENS, from the list's own
 * Emission Date range when one is set and from last month in Ecuador
 * otherwise, so the operator's last narrowing of the list is what it offers.
 * Only the range is taken from the list: the archive of a period is the same
 * whoever takes it and whatever the list shows.
 *
 * The download is a plain link to the BFF's proxy, the mechanism every other
 * invoice download uses: the browser saves the ZIP under the name the API
 * gives it, and a refusal comes back as the API's envelope.
 */
export function TaxDocumentArchiveDialog({ open, onOpenChange, listRange }: TaxDocumentArchiveDialogProps) {
  const t = useTranslations("operator.taxDocumentArchive");
  const [range, setRange] = useState<TaxDocumentArchiveRange>({ from: "", to: "" });
  const [summary, setSummary] = useState<{ rangeKey: string; state: SummaryState } | null>(null);

  const { issuedFrom, issuedTo } = listRange;
  useEffect(() => {
    if (open) {
      setRange(startingTaxDocumentArchiveRange({ issuedFrom, issuedTo }, new Date()));
      // A summary read on an earlier opening may be out of date by now.
      setSummary(null);
    }
  }, [open, issuedFrom, issuedTo]);

  const complete = isTaxDocumentArchiveRangeComplete(range);
  const inverted = isTaxDocumentArchiveRangeInverted(range);

  // The summary follows the range: read again whenever a complete range
  // changes. Each answer is kept with the range it was read for, so until the
  // current range's answer arrives the note says it is counting, and an
  // answer for a range the operator has since moved off (its request aborted
  // too) is never shown against the new dates.
  const { from, to } = range;
  const rangeKey = summaryRangeKey(range);
  useEffect(() => {
    if (!open || !isTaxDocumentArchiveRangeComplete({ from, to })) {
      return;
    }
    const controller = new AbortController();
    fetchOperatorTaxDocumentArchiveSummary({ from, to }, controller.signal)
      .then((loaded) => {
        if (!controller.signal.aborted) {
          setSummary({ rangeKey: summaryRangeKey({ from, to }), state: { status: "loaded", summary: loaded } });
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setSummary({ rangeKey: summaryRangeKey({ from, to }), state: { status: "error" } });
        }
      });
    return () => controller.abort();
  }, [open, from, to]);
  const summaryState: SummaryState = summary?.rangeKey === rangeKey ? summary.state : { status: "loading" };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("title")}</DialogTitle>
          <DialogDescription>{t("description")}</DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="tax-document-archive-from">{t("fromLabel")}</Label>
            <Input
              id="tax-document-archive-from"
              type="date"
              value={range.from}
              max={range.to || undefined}
              onChange={(event) => setRange((current) => ({ ...current, from: event.target.value }))}
              className="h-9"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="tax-document-archive-to">{t("toLabel")}</Label>
            <Input
              id="tax-document-archive-to"
              type="date"
              value={range.to}
              min={range.from || undefined}
              onChange={(event) => setRange((current) => ({ ...current, to: event.target.value }))}
              className="h-9"
            />
          </div>
        </div>

        {inverted ? <p className="text-sm text-destructive">{t("rangeInverted")}</p> : null}

        <p className="text-sm text-muted-foreground">{t("filtersNote")}</p>

        {complete ? <TaxDocumentArchiveSummaryNote state={summaryState} /> : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("cancel")}
          </Button>
          {complete ? (
            <Button asChild>
              <a href={taxDocumentArchiveUrl(range)} download>
                {t("download")}
              </a>
            </Button>
          ) : (
            <Button type="button" disabled>
              {t("download")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
