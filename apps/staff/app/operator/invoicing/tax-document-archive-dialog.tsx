"use client";

import { useEffect, useState } from "react";

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
} from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import {
  type TaxDocumentArchiveRange,
  isTaxDocumentArchiveRangeComplete,
  isTaxDocumentArchiveRangeInverted,
  startingTaxDocumentArchiveRange,
  taxDocumentArchiveUrl,
} from "@/lib/tax-document-archive";

type TaxDocumentArchiveDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The list's Emission Date range, "" for an open end, as the address bar holds it. */
  listRange: { issuedFrom: string; issuedTo: string };
};

/**
 * The Tax Document Archive's dialog (#630, spec #629): a From and a To
 * Emission Date, a note that the list's other filters don't apply, and the
 * download.
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

  const { issuedFrom, issuedTo } = listRange;
  useEffect(() => {
    if (open) {
      setRange(startingTaxDocumentArchiveRange({ issuedFrom, issuedTo }, new Date()));
    }
  }, [open, issuedFrom, issuedTo]);

  const complete = isTaxDocumentArchiveRangeComplete(range);
  const inverted = isTaxDocumentArchiveRangeInverted(range);

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

        {/*
          The pre-download summary (#631) goes here: the facturas and Credit
          Notes the range will hold, the unsettled warning and the
          empty-period message, fetched whenever the range changes.
        */}

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
