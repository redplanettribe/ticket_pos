"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Breadcrumb,
  Button,
  Card,
  CardContent,
  CardHeader,
  Input,
  Label,
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { SortableHeader } from "@/components/sortable-header";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, formatCalendarDay, formatMoney, formatNumber } from "@/lib/format";
import {
  type InvoiceEnvironmentFilter,
  type InvoiceKind,
  type InvoiceKindFilter,
  type InvoiceStatusFilter,
  type OperatorInvoiceListPage,
  type OperatorInvoiceListItem,
  fetchOperatorRecipientWarningCount,
  fetchOperatorUninvoicedHouseSaleCount,
} from "@/lib/operator-api";
import {
  INVOICE_ENVIRONMENT_FILTERS,
  INVOICE_KIND_FILTERS,
  INVOICE_STATUS_FILTERS,
  type OperatorInvoiceDir,
  type OperatorInvoiceFilters,
  type OperatorInvoiceSort,
  fetchOperatorInvoiceList,
  hasActiveOperatorInvoiceFilters,
  isDefaultOperatorInvoiceView,
  operatorInvoiceListQuery,
  operatorInvoiceListQueryAfterFilterChange,
  operatorInvoiceListQueryAfterReset,
  operatorInvoiceListQueryAfterSortChange,
} from "@/lib/operator-invoice-list";

import { CertificateExpiryBanner } from "./certificate-expiry-warning";
import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "./invoice-status";
import { TaxDocumentArchiveDialog } from "./tax-document-archive-dialog";

// The invoices list (#454): every factura the platform issued — number, date,
// Recipient, total, status, country, and a Test badge for the SRI pruebas
// environment so a certification run is never mistaken for a real factura.
// Newest first until the operator says otherwise: the Number, Date, Recipient
// and Total headers reorder the list (#597), and the order the page opens in
// is the one it has always had. From #473 it is also every document the platform OWES: a Sale
// Invoice or Credit Note appears the moment its sale commits, with its kind
// and its Sale Confirmation reference beside the manual Tax Invoices, and
// with no number or date until the Drainer signs it — those cells say so
// rather than showing a blank, since "not yet" and "unknown" are different.
// The kind filter (#477) narrows the list to one of the three, and the
// status filter (#578) to one of the nine; the environment filter (#598) to
// SRI producción or pruebas, so a month-end reconciliation never counts a
// certification document. The API does the narrowing, so the page's total is
// the filtered total, and one Reset clears every filter, the search, the
// range and the order at once.
//
// The Recipient Warning (#482, ADR 0061): an authorized Sale Invoice the SRI
// warned about — the Recipient's Tax ID does not exist or is incorrect — is
// marked in the list and found by a second filter. Both exist only while
// Sale Invoicing is open: the count endpoint is the tell, answering 404 while
// the feature is closed, and the filter is drawn only once it has answered.
//
// The Uninvoiced House Sales count (#509, ADR 0064) is read under the same
// rule: with the page, hidden while its endpoint answers 404, and shown as
// "0" when the backlog is clear — a zero is an answer, not an absence. It is
// NOT a filter, though, so it is drawn in the page's actions rather than
// among them: it opens Ventas sin factura, where the backfill lives.

// The Kind filter's and the Status filter's options (#477; #578, ADR 0068 —
// the status filter exists so that every number the platform has given up on
// can be audited, and offers the whole vocabulary rather than `abandoned`
// alone) live with the parser that reads them off the URL, so the select can
// never offer a value the address bar would refuse (#594).
//
// Every narrowing on this page is the URL's (#594), and so is the order
// (#597): the filters, the page and the sort arrive as props parsed from the
// address bar, and each change is a router.push of the rebuilt query string.
// Nothing here is seeded from useState, so a reload, a shared link and the
// back button all show the same view.

// Where the list lives: every filter change and page move is pushed onto it.
const LIST_PATH = "/operator/invoicing";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

// The environment filter's options, by catalog key (#598). Spelled out
// rather than derived from the badge's key, because the select and the badge
// say different things about the same word: the badge marks one row as a
// certification document, while these are the two halves of a choice and
// have to read as a pair.
const INVOICE_ENVIRONMENT_FILTER_KEYS = {
  all: "invoicingEnvironmentFilterAll",
  test: "invoicingEnvironmentFilterTest",
  production: "invoicingEnvironmentFilterProduction",
} as const satisfies Record<InvoiceEnvironmentFilter, string>;

function InvoiceRow({ item, locale }: { item: OperatorInvoiceListItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b last:border-b-0">
      <td className="whitespace-nowrap py-3 pr-4">
        <Link href={`/operator/invoicing/${item.id}`} className="font-mono font-medium hover:underline">
          {item.number ?? t("invoicingNotIssuedYet")}
        </Link>
        {item.environment === "test" ? (
          <Badge variant="outline" className="ml-2 align-middle">
            {t("invoicingTestBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="whitespace-nowrap py-3 pr-4">{t(INVOICE_KIND_KEYS[item.kind])}</td>
      <td className="py-3 pr-4 font-mono text-xs">{item.sale_confirmation_ref ?? "—"}</td>
      <td className="whitespace-nowrap py-3 pr-4 tabular-nums">
        {item.issued_on ? formatCalendarDay(item.issued_on, locale) : <span className="text-muted-foreground">—</span>}
      </td>
      <td className="py-3 pr-4">
        <p className="font-medium">{item.recipient.legal_name}</p>
        <p className="font-mono text-xs text-muted-foreground">{item.recipient.tax_id}</p>
      </td>
      <td className="py-3 pr-4 text-right tabular-nums">{formatMoney(item.total_cents, item.currency, locale)}</td>
      <td className="py-3 pr-4">
        <Badge variant={INVOICE_STATUS_VARIANTS[item.status]}>{t(INVOICE_STATUS_KEYS[item.status])}</Badge>
        {item.recipient_warning ? (
          // Beside the status, never in its place: the document is authorized
          // and the warning says something else about it.
          <Badge variant="outline" className="ml-2 border-amber-500 text-amber-700">
            {t("invoicingRecipientWarningBadge")}
          </Badge>
        ) : null}
        {item.superseded_by_invoice_id ? (
          // The superseded marker (#486, ADR 0061), beside the status for the
          // same reason: a reissued factura is still authorized, and no
          // longer the sale's current one — so the operator does not act on
          // it.
          <Badge variant="secondary" className="ml-2">
            {t("invoicingSupersededBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="py-3 pr-4 uppercase text-muted-foreground">{item.country}</td>
    </tr>
  );
}

// InvoiceSearchBox is the one box that finds a document by any of the four
// things an operator might be holding (#595): the printed number, the
// Recipient's legal name, the Recipient's Tax ID, or the Sale Confirmation
// reference of the Ticket Sale the document declares. What "match" means is
// the API's; this only carries the term.
//
// IT COMMITS ON SUBMIT, never on a keystroke — the Sales list's rule, and for
// its reason: every commit is a router.push, so typing would push a history
// entry per character and a half-typed term would narrow the list under the
// operator's hands.
//
// The local state is the BOX's, not the view's: the URL stays the source of
// truth, and the effect below re-seeds the box whenever the address bar moves
// under it (the back button, a shared link, or Clear).
function InvoiceSearchBox({
  q,
  onApply,
}: {
  q: string;
  onApply: (patch: Partial<OperatorInvoiceFilters>) => void;
}) {
  const t = useTranslations("operator");
  const [term, setTerm] = useState(q);

  useEffect(() => {
    setTerm(q);
  }, [q]);

  return (
    <form
      className="flex flex-col gap-1"
      onSubmit={(event) => {
        event.preventDefault();
        // Trimmed, so a term that is only whitespace clears the search rather
        // than narrowing the list to nothing. onApply returns to page one.
        onApply({ q: term.trim() });
      }}
    >
      <Label htmlFor="operator-invoice-search">
        {t("invoicingSearchLabel")}
      </Label>
      <div className="flex gap-2">
        <Input
          id="operator-invoice-search"
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          placeholder={t("invoicingSearchPlaceholder")}
          className="h-9"
        />
        <Button type="submit" variant="outline" size="sm">
          {t("invoicingSearchAction")}
        </Button>
        {/*
          Clear is drawn only while there is a search to clear, and empties
          the term rather than every filter — resetting the whole view is the
          Reset beside the selects (#598), and a Clear that quietly dropped
          the Kind an operator had set would be a different button wearing
          this one's label.
        */}
        {q ? (
          <Button type="button" variant="ghost" size="sm" onClick={() => onApply({ q: "" })}>
            {t("invoicingSearchClear")}
          </Button>
        ) : null}
      </div>
    </form>
  );
}

// InvoiceFilterBar is every narrowing of the list, in one panel above it.
//
// Its SHAPE is the Sales list's (`SalesFilterBar`), copied deliberately and
// not invented a second time: a bordered panel above the table, every label
// above its control, the controls in one responsive grid, and the buttons
// that undo a narrowing on a right-aligned row beneath. Two filter surfaces
// in the same app that look different teach the operator two habits for one
// job — the Holder List already settled this by copying that bar rather than
// growing its own. Only the LAYOUT is borrowed; the filters are this page's.
//
// Search takes two columns: it is the widest control and the only one
// carrying buttons inside it. The two Emission Date bounds sit beside it,
// because "this Recipient, in August" is one question. The four selects then
// fall into a single row of equal-width boxes.
//
// The Emission Date bounds (#596) are two inclusive calendar days on the day
// the Date column shows. Either may stand alone, so "everything since July
// 1st" is one input. They are APPLIED ON CHANGE, unlike the search box: a
// date input commits a whole date at once, so there is nothing for a submit
// button to wait for. They bound EACH OTHER — max on the start, min on the
// end — so the picker cannot offer an inverted range; the API refuses one
// anyway, since a hand-edited address bar reaches it without passing here.
//
// Clear dates empties BOTH bounds and nothing else — half a range is a
// different view, not a cleared one — and it sits beneath the grid rather
// than in a cell, because a button that clears two cells belongs to neither.
// Beside it, Reset (#598) clears the whole view. Each is drawn only while it
// has something to do, so the default view carries no button that would do
// nothing.
function InvoiceFilterBar({
  filters,
  recipientWarningCount,
  showReset,
  onApply,
  onReset,
}: {
  filters: OperatorInvoiceFilters;
  recipientWarningCount: number | null;
  showReset: boolean;
  onApply: (patch: Partial<OperatorInvoiceFilters>) => void;
  onReset: () => void;
}) {
  const t = useTranslations("operator");
  const rangeSet = Boolean(filters.issuedFrom || filters.issuedTo);

  return (
    <div className="space-y-3 rounded-md border bg-muted/20 p-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className="lg:col-span-2">
          <InvoiceSearchBox q={filters.q} onApply={onApply} />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="operator-invoice-issued-from">{t("invoicingIssuedFromLabel")}</Label>
          <Input
            id="operator-invoice-issued-from"
            type="date"
            value={filters.issuedFrom}
            max={filters.issuedTo || undefined}
            onChange={(event) => onApply({ issuedFrom: event.target.value })}
            className="h-9"
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="operator-invoice-issued-to">{t("invoicingIssuedToLabel")}</Label>
          <Input
            id="operator-invoice-issued-to"
            type="date"
            value={filters.issuedTo}
            min={filters.issuedFrom || undefined}
            onChange={(event) => onApply({ issuedTo: event.target.value })}
            className="h-9"
          />
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="operator-invoice-kind">{t("invoicingKindFilterLabel")}</Label>
          <select
            id="operator-invoice-kind"
            className={SELECT_CLASS}
            value={filters.kind}
            onChange={(event) => onApply({ kind: event.target.value as InvoiceKindFilter })}
          >
            {INVOICE_KIND_FILTERS.map((option) => (
              <option key={option} value={option}>
                {option === "all" ? t("invoicingKindFilterAll") : t(INVOICE_KIND_KEYS[option as InvoiceKind])}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="operator-invoice-status">{t("invoicingStatusFilterLabel")}</Label>
          <select
            id="operator-invoice-status"
            className={SELECT_CLASS}
            value={filters.status}
            onChange={(event) => onApply({ status: event.target.value as InvoiceStatusFilter })}
          >
            {INVOICE_STATUS_FILTERS.map((option) => (
              <option key={option} value={option}>
                {option === "all" ? t("invoicingStatusFilterAll") : t(INVOICE_STATUS_KEYS[option])}
              </option>
            ))}
          </select>
        </div>

        {/*
          The Environment filter (#598), a fourth select beside the other
          three: a month-end reconciliation is read under `production` alone,
          so a certification run against SRI pruebas — whose documents are
          real to the SRI and to nobody else — is never counted into it. It
          defaults to All, so the page an operator already knows is unchanged.

          There is no Country select beside it on purpose: there is one Issuer
          country, and a select with one option is a control that asks a
          question with no answer.
        */}
        <div className="flex flex-col gap-1">
          <Label htmlFor="operator-invoice-environment">{t("invoicingEnvironmentFilterLabel")}</Label>
          <select
            id="operator-invoice-environment"
            className={SELECT_CLASS}
            value={filters.environment}
            onChange={(event) => onApply({ environment: event.target.value as InvoiceEnvironmentFilter })}
          >
            {INVOICE_ENVIRONMENT_FILTERS.map((option) => (
              <option key={option} value={option}>
                {t(INVOICE_ENVIRONMENT_FILTER_KEYS[option])}
              </option>
            ))}
          </select>
        </div>

        {/*
          The Recipient Warning filter (#482) reads as a sentence carrying a
          count, not as a named field, so it takes a checkbox beside its words
          rather than a label above a box. It keeps the height of the controls
          in the other cells so the row still reads as one line of filters.
        */}
        {recipientWarningCount !== null ? (
          <div className="flex items-end">
            <label className="flex h-9 items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={filters.recipientWarningOnly}
                onChange={(event) => onApply({ recipientWarningOnly: event.target.checked })}
              />
              <span>{t("invoicingRecipientWarningFilter", { count: recipientWarningCount })}</span>
            </label>
          </div>
        ) : null}
      </div>

      {rangeSet || showReset ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          {rangeSet ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => onApply({ issuedFrom: "", issuedTo: "" })}
            >
              {t("invoicingIssuedRangeClear")}
            </Button>
          ) : null}
          {showReset ? (
            <Button type="button" variant="ghost" size="sm" onClick={onReset}>
              {t("invoicingResetView")}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function OperatorInvoicesClient({
  page,
  filters,
  sort,
  dir,
}: {
  page: number;
  filters: OperatorInvoiceFilters;
  sort: OperatorInvoiceSort;
  dir: OperatorInvoiceDir;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const router = useRouter();
  const [result, setResult] = useState<OperatorInvoiceListPage | null>(null);
  // null while unknown or while the feature is closed: the filter is not drawn.
  const [recipientWarningCount, setRecipientWarningCount] = useState<number | null>(null);
  // null while unknown or while the feature is closed: the link is not drawn.
  const [uninvoicedCount, setUninvoicedCount] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Whether the Tax Document Archive's dialog is open. Not the URL's: a
  // dialog is a moment, not a view anyone links to.
  const [archiveOpen, setArchiveOpen] = useState(false);

  // The canonical query of the view being shown: the fetch key below, so one
  // string decides both what is requested and when it is re-requested.
  const viewQuery = operatorInvoiceListQuery(page, filters, sort, dir);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // The count is read beside the page: it says whether Sale Invoicing is
      // open (404 while closed, read as "no filter") and how many the filter
      // would find. A closed feature must not take the list down with it.
      const [listPage, warningCount, uninvoiced] = await Promise.all([
        fetchOperatorInvoiceList(page, filters, sort, dir),
        fetchOperatorRecipientWarningCount().catch(() => null),
        fetchOperatorUninvoicedHouseSaleCount().catch(() => null),
      ]);
      setResult(listPage);
      setRecipientWarningCount(warningCount?.recipient_warning_count ?? null);
      setUninvoicedCount(uninvoiced?.uninvoiced_house_sale_count ?? null);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("invoicingListLoadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // viewQuery encodes the page and every filter — the whole fetch key — so
    // the list re-reads whenever the address bar moves, and only then.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewQuery]);

  useEffect(() => {
    void load();
  }, [load]);

  // Each control rewrites the address bar rather than a piece of local state:
  // the server re-renders this component with the new props, and the back
  // button walks the operator's filter history (#594).
  const applyFilters = useCallback(
    (patch: Partial<OperatorInvoiceFilters>) => {
      // Any narrowing returns to page one — a narrower result has fewer pages.
      // The order survives a narrowing: the operator chose it about the list,
      // not about the documents that happen to be in it.
      router.push(`${LIST_PATH}${operatorInvoiceListQueryAfterFilterChange(filters, patch, sort, dir)}`);
    },
    [filters, sort, dir, router],
  );

  // Pressing a column header reorders the list through the address bar like
  // everything else here: the active column flips, another column takes over,
  // and either way the list returns to page one, since page seven of a
  // reordered list holds different documents.
  const toggleSort = useCallback(
    (pressed: OperatorInvoiceSort) => {
      router.push(`${LIST_PATH}${operatorInvoiceListQueryAfterSortChange(filters, sort, dir, pressed)}`);
    },
    [filters, sort, dir, router],
  );

  // Reset (#598, spec #593 story 22): back to the whole list, first page,
  // newest first — the address the page opens at, with no query string at
  // all. It clears the ORDER as well as every filter, because the operator
  // is looking at one view rather than at a narrowing and an ordering, and a
  // Reset that left the list sorted by total ascending would not have taken
  // them back to where they started. That is why it asks
  // isDefaultOperatorInvoiceView rather than hasActiveOperatorInvoiceFilters,
  // which deliberately ignores the sort for the empty state's sake.
  const resetView = useCallback(() => {
    router.push(`${LIST_PATH}${operatorInvoiceListQueryAfterReset()}`);
  }, [router]);

  const goToPage = useCallback(
    (next: number) => {
      const target = Math.max(1, next);
      if (target === page) {
        return;
      }
      // Paging keeps the filters: only the page moves.
      router.push(`${LIST_PATH}${operatorInvoiceListQuery(target, filters, sort, dir)}`);
    },
    [filters, sort, dir, page, router],
  );

  const items = result?.data ?? [];
  const pagination = result?.pagination;

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("invoicingBreadcrumbList") },
        ]}
      />
      <PageHeader
        title={t("invoicingListTitle")}
        description={t("invoicingListDescription")}
        actions={
          <>
            {/*
              The Uninvoiced House Sales count (#509, ADR 0064) sits with the
              other ways OFF this page rather than among the filters, because it
              narrows nothing — it is a backlog, and a door to where the
              backfill lives. The rule for when it is drawn at all is with the
              other counts, at the top of this file.
            */}
            {uninvoicedCount !== null ? (
              <Button asChild variant="outline">
                <Link href="/operator/invoicing/uninvoiced">
                  {t("invoicingUninvoicedCountLink", { count: uninvoicedCount })}
                </Link>
              </Button>
            ) : null}
            {/*
              The Tax Document Archive (#630): the period's authorized
              production documents as signed XML, for the accountant. A
              dialog rather than a link, because it asks for its own range.
            */}
            <Button type="button" variant="outline" onClick={() => setArchiveOpen(true)}>
              {t("taxDocumentArchive.action")}
            </Button>
            <Button asChild variant="outline">
              <Link href="/operator/invoicing/issuer">{t("invoicingViewIssuer")}</Link>
            </Button>
            <Button asChild>
              <Link href="/operator/invoicing/new">{t("invoicingNewInvoice")}</Link>
            </Button>
          </>
        }
      />

      {/*
        The Certificate Expiry Warning (#504, ADR 0063), where the documents
        are: the same banner the Operator Dashboard carries, read on its own
        so a failed Issuer read never takes the list down with it.
      */}
      <CertificateExpiryBanner />

      <TaxDocumentArchiveDialog
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
        listRange={{ issuedFrom: filters.issuedFrom, issuedTo: filters.issuedTo }}
      />

      {forbidden ? (
        <Alert variant="destructive">
          <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
          <AlertDescription>{t("accessDenied")}</AlertDescription>
        </Alert>
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingListLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : loading ? (
        <p className="text-sm text-muted-foreground">{t("invoicingListLoading")}</p>
      ) : (
        <Card>
          {/*
            No CardTitle here: the PageHeader above already names this page and
            describes it, and the breadcrumb already places it. A second copy of
            the same two lines, one row below the first, said nothing the first
            had not. The Card is the table's frame, and the filters that narrow
            the table sit directly above it.
          */}
          <CardHeader>
            <InvoiceFilterBar
              filters={filters}
              recipientWarningCount={recipientWarningCount}
              showReset={!isDefaultOperatorInvoiceView(page, filters, sort, dir)}
              onApply={applyFilters}
              onReset={resetView}
            />
          </CardHeader>
          <CardContent>
            {items.length === 0 ? (
              // "No documents at all" and "nothing matches these filters" are
              // different answers and lead to different next moves — widen the
              // filters, or stop looking (#595). The unnarrowed case is asked
              // first, so the empty page only ever claims the platform has
              // issued nothing when nothing is narrowing the view. A search
              // spans four fields and combines with the rest, so once a term
              // is set the honest message is the general one rather than a
              // sentence about the Kind. An Emission Date range says the same
              // thing (#596): "no documents match these filters" is the
              // sentence that sends the operator to widen the window, where
              // "no documents of that kind" would blame the wrong control.
              <p className="text-sm text-muted-foreground">
                {!hasActiveOperatorInvoiceFilters(filters)
                  ? t("invoicingListEmpty")
                  : filters.q ||
                      filters.issuedFrom ||
                      filters.issuedTo ||
                      filters.environment !== "all"
                    ? t("invoicingListEmptyForFilters")
                    : filters.recipientWarningOnly
                      ? t("invoicingListEmptyForRecipientWarning")
                      : filters.status !== "all"
                        ? t("invoicingListEmptyForStatus")
                        : t("invoicingListEmptyForKind")}
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full border-collapse text-sm">
                  <thead>
                    {/*
                      Four of the eight headers reorder the list (#597), and
                      the other four stay plain on purpose: Kind, Status and
                      Country are answered by their filters — an operator
                      wants to see one of them alone rather than read a run of
                      them — and the Sale column is a reference nobody reads in
                      order. The header is the Sales list's, so a column that
                      sorts looks and behaves the same on both screens.
                    */}
                    <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <SortableHeader
                        label={t("invoicingColNumber")}
                        field="number"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                      />
                      <th className="py-2 pr-4 font-medium">{t("invoicingColKind")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColSale")}</th>
                      <SortableHeader
                        label={t("invoicingColDate")}
                        field="date"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                      />
                      <SortableHeader
                        label={t("invoicingColRecipient")}
                        field="recipient"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                      />
                      <SortableHeader
                        label={t("invoicingColTotal")}
                        field="total"
                        sort={sort}
                        dir={dir}
                        onSort={toggleSort}
                        numeric
                      />
                      <th className="py-2 pr-4 font-medium">{t("invoicingColStatus")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColCountry")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((item) => (
                      <InvoiceRow key={item.id} item={item} locale={locale} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/*
              The pagination control (#594): the list used to stop silently at
              fifty documents. Drawn whenever the narrowed result has any
              documents at all, because the total is the count the operator
              came for — the page buttons are what disable themselves when
              there is only one page. Written the way the Uninvoiced House
              Sales list beside it is, rather than by lifting the Sales list's
              control, which speaks the `sales` catalog; the page number moves
              through the URL, not through state.
            */}
            {pagination && pagination.total > 0 ? (
              <div className="mt-4 flex items-center justify-between border-t pt-4">
                <p className="text-sm text-muted-foreground">
                  {t("invoicingListPageSummary", {
                    page: formatNumber(pagination.page, locale),
                    pages: formatNumber(pagination.total_pages, locale),
                    total: pagination.total,
                  })}
                </p>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pagination.page <= 1}
                    onClick={() => goToPage(pagination.page - 1)}
                  >
                    {t("previousPage")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pagination.page >= pagination.total_pages}
                    onClick={() => goToPage(pagination.page + 1)}
                  >
                    {t("nextPage")}
                  </Button>
                </div>
              </div>
            ) : null}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
