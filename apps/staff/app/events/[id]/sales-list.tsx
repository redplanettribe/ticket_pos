"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { toAppLocale } from "@ticket-pos/locale";
import {
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
  Input,
  Label,
  Skeleton,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { SortableHeader } from "@/components/sortable-header";
import { apiErrorMessage } from "@/lib/api-errors";
import { dossierHref } from "@/lib/customer-dossier";
import { ApiError } from "@/lib/events-api";
import {
  NOTHING_TO_SHOW,
  PLATFORM_TIME_ZONE,
  formatDateTime,
  formatMoney,
  formatNumber,
} from "@/lib/format";
import {
  EMPTY_SALES_FILTERS,
  PAYMENT_METHODS,
  SALE_CHANNELS,
  SALE_SOURCES,
  canCorrectSale,
  canReverseSale,
  downloadSalesExport,
  exportFieldMessage,
  fetchSalesList,
  hasActiveSalesFilters,
  paymentMethodToken,
  reversalProvenance,
  reverseSale,
  saleChannelToken,
  saleOriginToken,
  saleSourceToken,
  salesListQuery,
  taxIdSnapshot,
  type PaymentMethod,
  type ReversalActor,
  type SaleChannel,
  type SaleListRow,
  type SaleOrigin,
  type SaleSortDir,
  type SaleSortField,
  type SaleSource,
  type SalesFilters,
  type SalesListResponse,
  type TaxIdType,
} from "@/lib/sales-api";

import { SaleCorrectionDialog } from "./sale-correction-dialog";
import { useSalesRefreshNotify, useSalesRefreshSignal } from "./sales-refresh";
import { TicketAnswersDialog } from "./ticket-answers-dialog";

/**
 * The token → catalog key maps for everything lib/sales-api.ts narrows.
 *
 * They live in the component and not in lib/ because they are the seam between a
 * decision and a word, and only this side of it may know about the catalog. They
 * are `as const` records rather than a template string, so `t()` is called with a
 * literal key and the compiler still checks it against en.json (global.d.ts) —
 * `t(\`channel${token}\`)` would type as `string` and silently allow a key that
 * does not exist.
 *
 * Every lookup is `token ? t(KEY[token]) : raw`. A value the backend adds
 * tomorrow narrows to null and renders as the API's own word, which is the same
 * floor ADR 0023 puts under an error code this app has not heard of.
 */
export const CHANNEL_KEYS = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
} as const satisfies Record<SaleChannel, string>;

export const SOURCE_KEYS = {
  direct: "sourceDirect",
  external_platform: "sourceExternalPlatform",
} as const satisfies Record<SaleSource, string>;

// How the sale reached the platform (#370, ADR 0052). The API derives it — the
// three-way negative that recognises a Manually Recorded Sale is single-sourced
// in Go — and this map is only the word for each answer.
export const ORIGIN_KEYS = {
  sale_import: "originSaleImport",
  manually_recorded: "originManuallyRecorded",
  correction_replacement: "originCorrectionReplacement",
  channel_sale: "originChannelSale",
} as const satisfies Record<SaleOrigin, string>;

export const PAYMENT_METHOD_KEYS = {
  cash: "paymentCash",
  transfer: "paymentTransfer",
  payphone: "paymentPayphone",
} as const satisfies Record<PaymentMethod, string>;

export const TAX_ID_KEYS = {
  cedula: "taxIdCedula",
  ruc: "taxIdRuc",
  passport: "taxIdPassport",
} as const satisfies Record<TaxIdType, string>;

const REVERSAL_ACTOR_KEYS = {
  customer: "actorCustomer",
  staff: "actorStaff",
  operator: "actorOperator",
} as const satisfies Record<ReversalActor, string>;

// TicketTypeOption is the minimal Ticket Type shape the ticket-type filter needs.
export type TicketTypeOption = {
  id: string;
  name: string;
};

type SalesListProps = {
  eventId: string;
  // The current page, read from the URL by the server and passed in as the
  // source of truth so page state lives in the URL.
  page: number;
  // The active filters, likewise read from the URL so the view is shareable and
  // survives a refresh.
  filters: SalesFilters;
  // The Event's Ticket Types, for the ticket-type filter dropdown.
  ticketTypes: TicketTypeOption[];
  // The current sort column and direction, also read from the URL so the sort is
  // shareable and survives a refresh.
  sort: SaleSortField;
  dir: SaleSortDir;
  // The Event timezone, which every time in this table is drawn in. Null on an
  // Event that names none, and then the platform's own clock rather than the
  // reader's laptop: the Staff Locale decides the marks around a time and
  // nothing whatever about which clock it is on (ADR 0041).
  timezone: string | null;
  // Whether the viewer may take the Sales Export. Org Admins and Event Owners
  // may; Event Staff may not, and are not shown the button rather than shown one
  // that refuses them — the API refuses them too.
  canExport: boolean;
  // Whether the viewer may reverse one imported Ticket Sale from its row (#350,
  // ADR 0050): the Sale Import's own gate. Org Admins and Event Owners may;
  // Event Staff see a reversed row's state and no action — the API refuses
  // them too.
  canManageSales: boolean;
  // The platform's Ticket Question feature flag, read off the Event payload
  // (#309, ADR 0045) — not a property of this Event. False is the shipped state,
  // and with it false no row offers an Answers button: every request behind one
  // would 404, and the answer to whether the feature exists lives in ONE place.
  ticketQuestionsEnabled: boolean;
  // Whether a buyer's name links to their Customer Dossier (#638): Org Admins
  // and Event Owners only. Event Staff get plain text, never a link the API
  // would refuse.
  canOpenDossier: boolean;
};

// defaultDirFor is the direction a newly selected sort column starts in: newest
// or largest first for the date/amount columns, A→Z for the customer column.
function defaultDirFor(field: SaleSortField): SaleSortDir {
  return field === "customer" ? "asc" : "desc";
}

export function SalesList({
  eventId,
  page,
  filters,
  ticketTypes,
  sort,
  dir,
  timezone,
  canExport,
  canManageSales,
  ticketQuestionsEnabled,
  canOpenDossier,
}: SalesListProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const router = useRouter();
  const notifySalesRefresh = useSalesRefreshNotify();
  // The sale a Reverse press is asking about, until it is confirmed or dismissed.
  const [reverseTarget, setReverseTarget] = useState<SaleListRow | null>(null);
  const [reversing, setReversing] = useState(false);
  // The sale a Correct press opened the form for (#351), until it is
  // committed or dismissed.
  const [correctTarget, setCorrectTarget] = useState<SaleListRow | null>(null);

  async function confirmReverse() {
    if (!reverseTarget) {
      return;
    }
    setReversing(true);
    try {
      const reversed = await reverseSale(eventId, reverseTarget.id);
      toast.success(t("reverseDone", { reference: reversed.confirmation_ref }));
      setReverseTarget(null);
      // The figures above and the import history beside it moved too: the same
      // signal a batch undo sends, so every sibling re-reads.
      notifySalesRefresh();
    } catch (reverseError) {
      toast.error(
        (reverseError instanceof ApiError ? apiErrorMessage(errorCopy, reverseError) : null) ??
          t("reverseFailed"),
      );
    } finally {
      setReversing(false);
    }
  }
  const [result, setResult] = useState<SalesListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  // Bumped by the import section on a successful commit/undo; re-runs the fetch
  // below against the current view (same page/searchParams) so the list reflects
  // the change without losing the owner's filters/sort/page.
  const refreshSignal = useSalesRefreshSignal();

  // The URL query is the fetch key: refetch whenever the page, any filter, or the
  // sort changes (the server re-renders these props from the address bar).
  const filterKey = salesListQuery(page, filters, sort, dir);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchSalesList(eventId, page, filters, sort, dir)
      .then((data) => {
        if (!cancelled) {
          setResult(data);
        }
      })
      .catch((fetchError: unknown) => {
        if (!cancelled) {
          // The API's verdict in this reader's language when the catalog knows
          // the code, the API's own English beneath that, and this surface's own
          // sentence when the request never reached the API at all.
          setError(
            (fetchError instanceof ApiError ? apiErrorMessage(errorCopy, fetchError) : null) ??
              t("loadFailed"),
          );
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // filterKey encodes eventId-independent page+filter+sort state; eventId is
    // listed explicitly so a different Event also refetches, and refreshSignal so
    // a commit/undo re-fetches the current view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, filterKey, refreshSignal]);

  const goToPage = useCallback(
    (next: number) => {
      const target = Math.max(1, next);
      if (target === page) {
        return;
      }
      // Page state lives in the URL so the view survives a refresh and is shareable;
      // the current filters and sort are preserved as we page.
      router.push(`/events/${eventId}/sales${salesListQuery(target, filters, sort, dir)}`);
    },
    [eventId, page, filters, sort, dir, router],
  );

  const toggleSort = useCallback(
    (field: SaleSortField) => {
      // Clicking the active column flips its direction; a new column starts in its
      // natural default direction. Sorting resets to page 1 since the order changes,
      // but the active filters are preserved.
      const nextDir: SaleSortDir =
        field === sort ? (dir === "asc" ? "desc" : "asc") : defaultDirFor(field);
      router.push(`/events/${eventId}/sales${salesListQuery(1, filters, field, nextDir)}`);
    },
    [eventId, filters, sort, dir, router],
  );

  // applyFilters merges a filter change into the URL, resetting to page 1 (a new
  // filter starts a fresh result set) while preserving the current sort. Each
  // change is a history entry so the back button steps through filter changes.
  const applyFilters = useCallback(
    (patch: Partial<SalesFilters>) => {
      router.push(`/events/${eventId}/sales${salesListQuery(1, { ...filters, ...patch }, sort, dir)}`);
    },
    [eventId, filters, sort, dir, router],
  );

  const filtersActive = hasActiveSalesFilters(filters);

  function toggleRow(id: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }

  const pagination = result?.pagination;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("title")}</CardTitle>
        <CardDescription>{t("description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {result ? (
          <ReversedSalesNotice
            count={result.reversed_count}
            viewingReversed={filters.status === "reversed"}
            onApply={applyFilters}
          />
        ) : null}
        <SalesFilterBar
          eventId={eventId}
          filters={filters}
          ticketTypes={ticketTypes}
          filtersActive={filtersActive}
          onApply={applyFilters}
          sort={sort}
          dir={dir}
          canExport={canExport}
        />
        {error ? (
          <p role="alert" className="rounded-md border border-destructive/50 p-4 text-sm text-destructive">
            {t("loadError", { message: error })}
          </p>
        ) : loading ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-12 w-full" />
            ))}
          </div>
        ) : !result || result.data.length === 0 ? (
          <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            {filtersActive ? t("emptyFiltered") : t("emptyNone")}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="w-8 py-1.5 pr-2" />
                  <SortableHeader
                    label={t("colCustomer")}
                    field="customer"
                    sort={sort}
                    dir={dir}
                    onSort={toggleSort}
                    dense
                  />
                  <th className="py-1.5 pr-4 font-medium">{t("colTaxId")}</th>
                  <th className="py-1.5 pr-4 font-medium">{t("colTicketTypes")}</th>
                  <SortableHeader
                    label={t("colAmount")}
                    field="amount"
                    sort={sort}
                    dir={dir}
                    onSort={toggleSort}
                    dense
                  />
                  <SortableHeader
                    label={t("colSold")}
                    field="sold_at"
                    sort={sort}
                    dir={dir}
                    onSort={toggleSort}
                    dense
                  />
                  <SortableHeader
                    label={t("colRecorded")}
                    field="recorded_at"
                    sort={sort}
                    dir={dir}
                    onSort={toggleSort}
                    dense
                  />
                  <th className="py-1.5 pr-4 font-medium">{t("colChannel")}</th>
                  <th className="py-1.5 pr-4 font-medium">{t("colReference")}</th>
                </tr>
              </thead>
              {result.data.map((sale) => (
                <SaleRows
                  key={sale.id}
                  sale={sale}
                  timezone={timezone}
                  locale={locale}
                  expanded={expanded.has(sale.id)}
                  onToggle={() => toggleRow(sale.id)}
                  ticketQuestionsEnabled={ticketQuestionsEnabled}
                  eventId={eventId}
                  canReverse={canReverseSale(canManageSales, sale)}
                  onReverse={() => setReverseTarget(sale)}
                  canCorrect={canCorrectSale(canManageSales, sale)}
                  onCorrect={() => setCorrectTarget(sale)}
                  dossierLink={
                    canOpenDossier
                      ? dossierHref(eventId, sale.customer_id, `/events/${eventId}/sales${filterKey}`)
                      : null
                  }
                />
              ))}
            </table>
          </div>
        )}

        {pagination && pagination.total > 0 ? (
          <PaginationControls
            page={pagination.page}
            totalPages={pagination.total_pages}
            total={pagination.total}
            onGo={goToPage}
          />
        ) : null}
      </CardContent>
      {/* Reversing one imported sale (#350, ADR 0050). The dialog states the
          one consequence the organizer cannot see from the row: how many
          accepted Holders lose their Ticket and are told. The buyer is mailed
          nothing, and the copy says so rather than offering a toggle. */}
      <Dialog
        open={reverseTarget !== null}
        onOpenChange={(open) => (!open ? setReverseTarget(null) : undefined)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reverseTitle")}</DialogTitle>
            <DialogDescription>
              {reverseTarget
                ? t("reverseBody", {
                    reference: reverseTarget.confirmation_ref,
                    count: reverseTarget.held_ticket_count,
                  })
                : null}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">{t("reverseBuyerSilent")}</p>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={reversing}
              onClick={() => setReverseTarget(null)}
            >
              {t("reverseCancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={reversing}
              onClick={() => void confirmReverse()}
            >
              {reversing ? t("reversing") : t("reverseConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* Correcting one imported sale (#351, ADR 0050): reverse plus a
          replacement in one act, from a form pre-filled with the row. */}
      <SaleCorrectionDialog
        eventId={eventId}
        sale={correctTarget}
        ticketTypes={ticketTypes}
        timezone={timezone}
        onClose={() => setCorrectTarget(null)}
        onCorrected={() => {
          setCorrectTarget(null);
          notifySalesRefresh();
        }}
      />
    </Card>
  );
}

type ReversedSalesNoticeProps = {
  // The Event's reversed Ticket Sales, whole-Event and filter-independent.
  count: number;
  // Whether the list is already showing them (status filter set to reversed).
  viewingReversed: boolean;
  onApply: (patch: Partial<SalesFilters>) => void;
};

// ReversedSalesNotice tells an organizer that Sale Reversals happened, above the
// list that no longer contains them.
//
// It exists because the default view shows active sales only: a buyer undoing
// their own Online Sale (ADR 0018) drops the total with no staff action and,
// without this line, nothing on the page would say why. Nobody is emailed — that
// is the ADR's deliberate choice — so this surface is the whole of the telling.
//
// It is a sentence, not a stat tile: on an Event that has never had a reversal
// it must not read as an alarm, and the count sits in the Sales list rather than
// the Net Proceeds strip above it because Event Staff see the list and not the
// strip. Showing them is the status filter the list already has, pre-set — one
// mechanism, and the resulting view is the ordinary shareable filtered URL.
function ReversedSalesNotice({ count, viewingReversed, onApply }: ReversedSalesNoticeProps) {
  const t = useTranslations("sales");

  if (count <= 0) {
    return <p className="text-xs text-muted-foreground">{t("reversedNoticeNone")}</p>;
  }

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm">
      <span>
        {/* "Reversed", never "voided", "cancelled" or "refunded": a Sale
            Reversal is the platform's own word for this, and a Cancelled Event
            is a different thing entirely (CONTEXT.md). The count is emphasised
            through a rich-text tag rather than by splitting the sentence in two,
            so the Spanish is free to put the emphasised part where its own
            grammar wants it. */}
        {t.rich("reversedNotice", {
          count,
          value: (chunks) => <span className="font-medium">{chunks}</span>,
        })}
      </span>
      {viewingReversed ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="ml-auto"
          onClick={() => onApply({ status: "active" })}
        >
          {t("backToActive")}
        </Button>
      ) : (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="ml-auto"
          onClick={() => onApply({ status: "reversed" })}
        >
          {t("showReversed")}
        </Button>
      )}
    </div>
  );
}

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm";

type SalesFilterBarProps = {
  eventId: string;
  filters: SalesFilters;
  ticketTypes: TicketTypeOption[];
  filtersActive: boolean;
  onApply: (patch: Partial<SalesFilters>) => void;
  sort: SaleSortField;
  dir: SaleSortDir;
  canExport: boolean;
};

// SalesFilterBar renders the filter controls. Selects and dates apply on change
// (each a URL/history step); the search box applies on submit so typing does not
// flood the history. All changes flow up through onApply, which drives the URL.
//
// The Download button lives here, among the filters it obeys, so what pressing
// it will produce is obvious before it is pressed.
//
// The option lists are built from the VALUE sets in lib/sales-api.ts and worded
// here. That keeps one list of what a Sales Channel can be — the same one the
// rows narrow against — while the words stay where every other word on this
// surface is.
function SalesFilterBar({
  eventId,
  filters,
  ticketTypes,
  filtersActive,
  onApply,
  sort,
  dir,
  canExport,
}: SalesFilterBarProps) {
  const t = useTranslations("sales");
  const [search, setSearch] = useState(filters.q);

  // Keep the search box in sync when the URL changes underneath us (e.g. the
  // back button or a Clear).
  useEffect(() => {
    setSearch(filters.q);
  }, [filters.q]);

  return (
    <div className="space-y-3 rounded-md border bg-muted/20 p-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <form
          className="flex flex-col gap-1"
          onSubmit={(event) => {
            event.preventDefault();
            onApply({ q: search.trim() });
          }}
        >
          <Label htmlFor="sales-search">{t("searchLabel")}</Label>
          <div className="flex gap-2">
            <Input
              id="sales-search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("searchPlaceholder")}
              className="h-9"
            />
            <Button type="submit" variant="outline" size="sm">
              {t("searchAction")}
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-status">{t("statusLabel")}</Label>
          <select
            id="sales-status"
            className={SELECT_CLASS}
            value={filters.status}
            onChange={(event) => onApply({ status: event.target.value })}
          >
            <option value="active">{t("statusActive")}</option>
            <option value="reversed">{t("statusReversed")}</option>
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-ticket-type">{t("ticketTypeLabel")}</Label>
          <select
            id="sales-ticket-type"
            className={SELECT_CLASS}
            value={filters.ticketTypeId}
            onChange={(event) => onApply({ ticketTypeId: event.target.value })}
          >
            <option value="">{t("allTicketTypes")}</option>
            {/* A Ticket Type's name is the Organization's own word and reads as
                coined in both languages — it is data, not copy. */}
            {ticketTypes.map((type) => (
              <option key={type.id} value={type.id}>
                {type.name}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-channel">{t("channelLabel")}</Label>
          <select
            id="sales-channel"
            className={SELECT_CLASS}
            value={filters.channel}
            onChange={(event) => onApply({ channel: event.target.value })}
          >
            <option value="">{t("allChannels")}</option>
            {SALE_CHANNELS.map((channel) => (
              <option key={channel} value={channel}>
                {t(CHANNEL_KEYS[channel])}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-source">{t("sourceLabel")}</Label>
          <select
            id="sales-source"
            className={SELECT_CLASS}
            value={filters.source}
            onChange={(event) => onApply({ source: event.target.value })}
          >
            <option value="">{t("allSources")}</option>
            {SALE_SOURCES.map((source) => (
              <option key={source} value={source}>
                {t(SOURCE_KEYS[source])}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-payment-method">{t("paymentMethodLabel")}</Label>
          <select
            id="sales-payment-method"
            className={SELECT_CLASS}
            value={filters.paymentMethod}
            onChange={(event) => onApply({ paymentMethod: event.target.value })}
          >
            <option value="">{t("allPaymentMethods")}</option>
            {PAYMENT_METHODS.map((method) => (
              <option key={method} value={method}>
                {t(PAYMENT_METHOD_KEYS[method])}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-sold-from">{t("soldFromLabel")}</Label>
          <Input
            id="sales-sold-from"
            type="date"
            value={filters.soldFrom}
            max={filters.soldTo || undefined}
            onChange={(event) => onApply({ soldFrom: event.target.value })}
            className="h-9"
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="sales-sold-to">{t("soldToLabel")}</Label>
          <Input
            id="sales-sold-to"
            type="date"
            value={filters.soldTo}
            min={filters.soldFrom || undefined}
            onChange={(event) => onApply({ soldTo: event.target.value })}
            className="h-9"
          />
        </div>
      </div>

      {filtersActive || canExport ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          {filtersActive ? (
            <Button type="button" variant="ghost" size="sm" onClick={() => onApply(EMPTY_SALES_FILTERS)}>
              {t("clearFilters")}
            </Button>
          ) : null}
          {canExport ? <SalesExportButton eventId={eventId} filters={filters} sort={sort} dir={dir} /> : null}
        </div>
      ) : null}
    </div>
  );
}

type SalesExportButtonProps = {
  eventId: string;
  filters: SalesFilters;
  sort: SaleSortField;
  dir: SaleSortDir;
};

// SalesExportButton downloads the Sales Export for the filters currently on
// screen. It fetches a blob rather than linking to the endpoint, because the
// endpoint answers with a file on success and a JSON error envelope on failure:
// a plain link would send the browser to raw JSON on a refusal, and the error
// would go unseen. The spinner and the inline message both follow from that —
// generation can take a moment, and the complaint belongs beside the filters
// that are the way to fix it.
function SalesExportButton({ eventId, filters, sort, dir }: SalesExportButtonProps) {
  const t = useTranslations("sales");
  const errorCopy = useMessages().errors;
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDownload() {
    setDownloading(true);
    setError(null);
    try {
      await downloadSalesExport(eventId, filters, sort, dir);
    } catch (downloadError: unknown) {
      // Three rungs, in this order and for a reason.
      //
      // The FIELD error's own sentence comes first, and it is the one message on
      // these surfaces that deliberately stays the API's English: the row-cap
      // refusal names how many sales matched and how many may travel at once,
      // and those numbers reach this app only inside that prose. A translated
      // sentence here would be Spanish with the actionable fact deleted, which is
      // worse for the reader than English with it (ADR 0023's floor exists for
      // exactly this). Then the catalog by code, then this surface's own words
      // for a request that never reached the API at all.
      const apiError = downloadError instanceof ApiError ? downloadError : null;
      setError(
        exportFieldMessage(apiError?.details) ??
          apiErrorMessage(errorCopy, apiError) ??
          t("exportFailed"),
      );
    } finally {
      setDownloading(false);
    }
  }

  return (
    <>
      {/* Its own full-width line inside the wrapping row: the refusal that
          matters here is a whole sentence naming a count and pointing back at
          the filters, and squeezed beside the button it would be a column of
          two words. It stays in this row so it reads as an answer to the
          button, right where the filters that caused it are. */}
      {error ? (
        <span role="alert" className="basis-full text-right text-sm text-destructive">
          {error}
        </span>
      ) : null}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={downloading}
        aria-busy={downloading}
        onClick={handleDownload}
      >
        {downloading ? t("preparingDownload") : t("downloadXlsx")}
      </Button>
    </>
  );
}

type SaleRowsProps = {
  sale: SaleListRow;
  timezone: string | null;
  locale: ReturnType<typeof toAppLocale>;
  expanded: boolean;
  onToggle: () => void;
  ticketQuestionsEnabled: boolean;
  eventId: string;
  // Whether this row offers Reverse: decided by the list from the viewer's
  // permission and the row's channel and status (lib/sales-api canReverseSale).
  canReverse: boolean;
  onReverse: () => void;
  // Whether this row offers Correct (#351): the same gate as Reverse.
  canCorrect: boolean;
  onCorrect: () => void;
  // The buyer's Customer Dossier, carrying this view back (#638); null draws
  // the name as plain text.
  dossierLink: string | null;
};

function SaleRows({
  sale,
  timezone,
  locale,
  expanded,
  onToggle,
  ticketQuestionsEnabled,
  eventId,
  canReverse,
  onReverse,
  canCorrect,
  onCorrect,
  dossierLink,
}: SaleRowsProps) {
  const t = useTranslations("sales");
  const tDossier = useTranslations("customerDossier");
  // The Answers dialog opens from the row detail rather than from the row: it is
  // a second surface about the same sale, and a button in the row itself would
  // compete with the expand for the same click.
  const [answersOpen, setAnswersOpen] = useState(false);
  // A Customer's name is data and is never translated. The zone is the Event's,
  // and the platform's clock beneath it — never the reader's machine.
  const name = `${sale.customer_first_name} ${sale.customer_last_name}`.trim() || NOTHING_TO_SHOW;
  const reversed = sale.status !== "active";
  const zone = timezone ?? PLATFORM_TIME_ZONE;
  const taxId = taxIdSnapshot(sale.tax_id_type, sale.tax_id_number);
  const channelToken = saleChannelToken(sale.channel);
  const sourceToken = saleSourceToken(sale.source);
  const channel = channelToken ? t(CHANNEL_KEYS[channelToken]) : sale.channel;
  const originToken = saleOriginToken(sale.origin);
  // How this row got here, under the channel it got here on. An origin this app
  // cannot name falls back to the API's own word, and a row that carries none at
  // all says nothing rather than inventing a route (#370).
  const origin = originToken ? t(ORIGIN_KEYS[originToken]) : sale.origin;
  const paymentToken = paymentMethodToken(sale.payment_method);

  return (
    <tbody className="border-b last:border-b-0">
      <tr
        className="cursor-pointer align-top hover:bg-muted/50"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <td className="py-1.5 pr-2 text-muted-foreground">{expanded ? "▾" : "▸"}</td>
        <td className="py-1.5 pr-4 leading-tight">
          <div className="font-medium">
            {dossierLink ? (
              <Link
                href={dossierLink}
                className="underline-offset-2 hover:underline"
                aria-label={tDossier("openDossier", { name })}
                // The row expands on click; following the link must not.
                onClick={(event) => event.stopPropagation()}
              >
                {name}
              </Link>
            ) : (
              name
            )}
          </div>
          <div className="text-muted-foreground">{sale.customer_email}</div>
          {/* A reversed sale says when it went and which side asked; an active
              one shows nothing extra (#117). */}
          {reversed ? (
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <Badge variant="destructive">
                {sale.replaced_by_confirmation_ref ? t("correctedBadge") : t("reversedBadge")}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {/* A corrected sale names its replacement first (#351): the
                    reference is what the reader goes looking for next. */}
                {sale.replaced_by_confirmation_ref ? (
                  <>
                    <span className="font-mono">
                      {t("correctedTo", { reference: sale.replaced_by_confirmation_ref })}
                    </span>
                    {" · "}
                  </>
                ) : null}
                <ReversalProvenanceText sale={sale} zone={zone} locale={locale} />
              </span>
            </div>
          ) : null}
          {/* The replacement says what it stands in for, on an active row too. */}
          {sale.replaces_confirmation_ref ? (
            <div className="mt-1 text-xs text-muted-foreground">
              <span className="font-mono">
                {t("corrects", { reference: sale.replaces_confirmation_ref })}
              </span>
            </div>
          ) : null}
        </td>
        <td className="py-1.5 pr-4 whitespace-nowrap">
          {taxId
            ? t("taxIdValue", {
                type: taxId.token ? t(TAX_ID_KEYS[taxId.token]) : taxId.rawType,
                number: taxId.number,
              })
            : NOTHING_TO_SHOW}
        </td>
        <td className="py-1.5 pr-4">
          {sale.ticket_types.length === 0
            ? NOTHING_TO_SHOW
            : sale.ticket_types
                .map((type) =>
                  t("ticketTypeQuantity", {
                    quantity: formatNumber(type.quantity, locale),
                    name: type.ticket_type_name,
                  }),
                )
                .join(", ")}
        </td>
        <td className="py-1.5 pr-4 tabular-nums">
          {formatMoney(sale.amount_cents, sale.currency, locale)}
        </td>
        <td className="py-1.5 pr-4">{formatDateTime(sale.sold_at, zone, locale)}</td>
        <td className="py-1.5 pr-4 text-muted-foreground">
          {formatDateTime(sale.recorded_at, zone, locale)}
        </td>
        <td className="py-1.5 pr-4 leading-tight">
          <div>
            {sourceToken || sale.source
              ? t("channelWithSource", {
                  channel,
                  source: sourceToken ? t(SOURCE_KEYS[sourceToken]) : (sale.source ?? ""),
                })
              : channel}
          </div>
          {/* The origin sits under the channel because it answers the next
              question that cell raises: "Import · Direct" says the Organization
              recorded this sale itself, and this says by which route — an
              uploaded file, somebody typing, or a Sale Correction standing in
              for a mistake. Read-only, and offered to every Member who may read
              the list at all (#370). */}
          {origin ? <div className="text-xs text-muted-foreground">{origin}</div> : null}
        </td>
        <td className="py-1.5 pr-4 font-mono text-xs">{sale.confirmation_ref}</td>
      </tr>
      {expanded ? (
        <tr className="bg-muted/30">
          <td />
          <td className="py-1.5 pr-4 text-muted-foreground" colSpan={8}>
            <div className="flex flex-wrap items-center gap-x-8 gap-y-1">
              <span>
                <span className="font-medium text-foreground">{t("paymentMethodHeading")}</span>{" "}
                {paymentToken
                  ? t(PAYMENT_METHOD_KEYS[paymentToken])
                  : (sale.payment_method ?? NOTHING_TO_SHOW)}
              </span>
              {/* The sale's Tickets and what each of them answered (#310).
                  Offered only where the flag is on, because every request behind
                  it would 404 while it is off (ADR 0045) — and offered on a
                  REVERSED sale too, because a Sale Reversal voids a sale and
                  does not erase what its Tickets said. The dialog refuses the
                  writes and shows the reason. */}
              {ticketQuestionsEnabled ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setAnswersOpen(true)}
                >
                  {t("ticketAnswers")}
                </Button>
              ) : null}
              {/* Reverse sits in the row detail for the same reason Answers does:
                  a destructive button in the row itself would compete with the
                  expand for the click. Offered only on an active imported sale
                  to a Member who may manage the Event's sales (#350). */}
              {canReverse ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    onReverse();
                  }}
                >
                  {t("reverse")}
                </Button>
              ) : null}
              {/* Correct: the same gate, the same place (#351). */}
              {canCorrect ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    onCorrect();
                  }}
                >
                  {t("correct")}
                </Button>
              ) : null}
            </div>
            {ticketQuestionsEnabled ? (
              <TicketAnswersDialog
                open={answersOpen}
                onOpenChange={setAnswersOpen}
                eventId={eventId}
                ticketSaleId={sale.id}
                confirmationRef={sale.confirmation_ref}
              />
            ) : null}
          </td>
        </tr>
      ) : null}
    </tbody>
  );
}

/**
 * How a reversed sale came to be reversed: when, and which side asked.
 *
 * Three states rather than a built-up string, because they are three sentences —
 * and because the moment and the actor sit in a different order in Spanish than
 * in English, which is precisely what interpolating rather than concatenating is
 * for. A sale reversed before the platform recorded either half says so plainly
 * rather than guessing a time: history is never backfilled.
 */
function ReversalProvenanceText({
  sale,
  zone,
  locale,
}: {
  sale: SaleListRow;
  zone: string;
  locale: ReturnType<typeof toAppLocale>;
}) {
  const t = useTranslations("sales");
  const provenance = reversalProvenance(sale.reversed_at, sale.reversed_by);

  if (provenance.state === "unrecorded") {
    return <>{t("reversalUnrecorded")}</>;
  }
  const when = formatDateTime(provenance.at, zone, locale) ?? NOTHING_TO_SHOW;
  if (provenance.state === "when") {
    return <>{when}</>;
  }
  return (
    <>
      {t("reversalBy", {
        when,
        actor: provenance.actorToken
          ? t(REVERSAL_ACTOR_KEYS[provenance.actorToken])
          : provenance.rawActor,
      })}
    </>
  );
}

type PaginationControlsProps = {
  page: number;
  totalPages: number;
  total: number;
  onGo: (page: number) => void;
};

function PaginationControls({ page, totalPages, total, onGo }: PaginationControlsProps) {
  const t = useTranslations("sales");
  const locale = toAppLocale(useLocale());
  const [jumpValue, setJumpValue] = useState("");

  function handleJump() {
    const target = Number.parseInt(jumpValue, 10);
    if (Number.isFinite(target)) {
      onGo(Math.min(Math.max(1, target), totalPages));
    }
    setJumpValue("");
  }

  return (
    <div className="flex flex-col gap-3 border-t pt-4 text-sm sm:flex-row sm:items-center sm:justify-between">
      <span className="text-muted-foreground">
        {/* One message, not a count glued to " · page " glued to a number: the
            plural of "sale" and the word order around the page numbers are both
            the translator's to decide. */}
        {t("pageSummary", {
          count: total,
          page: formatNumber(page, locale),
          pages: formatNumber(totalPages, locale),
        })}
      </span>
      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => onGo(page - 1)}>
          {t("previousPage")}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onGo(page + 1)}
        >
          {t("nextPage")}
        </Button>
        {totalPages > 1 ? (
          <form
            className="flex items-center gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              handleJump();
            }}
          >
            <Input
              type="number"
              min={1}
              max={totalPages}
              value={jumpValue}
              onChange={(event) => setJumpValue(event.target.value)}
              placeholder={t("jumpPlaceholder")}
              className="h-8 w-20"
              aria-label={t("jumpLabel")}
            />
            <Button type="submit" variant="outline" size="sm">
              {t("jumpGo")}
            </Button>
          </form>
        ) : null}
      </div>
    </div>
  );
}
