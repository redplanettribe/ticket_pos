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
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, formatCalendarDay, formatMoney, formatNumber } from "@/lib/format";
import {
  type InvoiceKind,
  type InvoiceKindFilter,
  type InvoiceStatusFilter,
  type OperatorInvoiceListPage,
  type OperatorInvoiceListItem,
  fetchOperatorRecipientWarningCount,
  fetchOperatorUninvoicedHouseSaleCount,
} from "@/lib/operator-api";
import {
  INVOICE_KIND_FILTERS,
  INVOICE_STATUS_FILTERS,
  type OperatorInvoiceFilters,
  fetchOperatorInvoiceList,
  hasActiveOperatorInvoiceFilters,
  operatorInvoiceListQuery,
  operatorInvoiceListQueryAfterFilterChange,
} from "@/lib/operator-invoice-list";

import { CertificateExpiryBanner } from "./certificate-expiry-warning";
import { INVOICE_KIND_KEYS, INVOICE_STATUS_KEYS, INVOICE_STATUS_VARIANTS } from "./invoice-status";

// The invoices list (#454): every factura the platform issued, newest first —
// number, date, Recipient, total, status, country, and a Test badge for the
// SRI pruebas environment so a certification run is never mistaken for a real
// factura. From #473 it is also every document the platform OWES: a Sale
// Invoice or Credit Note appears the moment its sale commits, with its kind
// and its Sale Confirmation reference beside the manual Tax Invoices, and
// with no number or date until the Drainer signs it — those cells say so
// rather than showing a blank, since "not yet" and "unknown" are different.
// The kind filter (#477) narrows the list to one of the three, and the
// status filter (#578) to one of the nine; the API does the narrowing, so
// the page's total is the filtered total.
//
// The Recipient Warning (#482, ADR 0061): an authorized Sale Invoice the SRI
// warned about — the Recipient's Tax ID does not exist or is incorrect — is
// marked in the list and found by a second filter. Both exist only while
// Sale Invoicing is open: the count endpoint is the tell, answering 404 while
// the feature is closed, and the filter is drawn only once it has answered.
//
// The Uninvoiced House Sales count (#509, ADR 0064) sits beside it under the
// same rule: read with the page, hidden while its endpoint answers 404, and
// shown as "0" when the backlog is clear — a zero is an answer, not an
// absence. It opens Ventas sin factura, where the backfill lives.

// The Kind filter's and the Status filter's options (#477; #578, ADR 0068 —
// the status filter exists so that every number the platform has given up on
// can be audited, and offers the whole vocabulary rather than `abandoned`
// alone) live with the parser that reads them off the URL, so the select can
// never offer a value the address bar would refuse (#594).
//
// Every narrowing on this page is the URL's (#594): the three filters and the
// page arrive as props parsed from the address bar, and each change is a
// router.push of the rebuilt query string. Nothing here is seeded from
// useState, so a reload, a shared link and the back button all show the same
// view.

// Where the list lives: every filter change and page move is pushed onto it.
const LIST_PATH = "/operator/invoicing";

const SELECT_CLASS =
  "h-9 rounded-md border border-input bg-background px-3 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

function InvoiceRow({ item, locale }: { item: OperatorInvoiceListItem; locale: AppLocale }) {
  const t = useTranslations("operator");
  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        <Link href={`/operator/invoicing/${item.id}`} className="font-mono font-medium hover:underline">
          {item.number ?? t("invoicingNotIssuedYet")}
        </Link>
        {item.environment === "test" ? (
          <Badge variant="outline" className="ml-2 align-middle">
            {t("invoicingTestBadge")}
          </Badge>
        ) : null}
      </td>
      <td className="py-3 pr-4">{t(INVOICE_KIND_KEYS[item.kind])}</td>
      <td className="py-3 pr-4 font-mono text-xs">{item.sale_confirmation_ref ?? "—"}</td>
      <td className="py-3 pr-4 tabular-nums">
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
      <Label htmlFor="operator-invoice-search" className="text-muted-foreground">
        {t("invoicingSearchLabel")}
      </Label>
      <div className="flex gap-2">
        <Input
          id="operator-invoice-search"
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          placeholder={t("invoicingSearchPlaceholder")}
          className="h-9 sm:w-72"
        />
        <Button type="submit" variant="outline" size="sm">
          {t("invoicingSearchAction")}
        </Button>
        {/*
          Clear is drawn only while there is a search to clear, and empties
          the term rather than every filter — resetting the whole view is
          #598's, and a Clear that quietly dropped the Kind an operator had
          set would be a different button wearing this one's label.
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

export function OperatorInvoicesClient({
  page,
  filters,
}: {
  page: number;
  filters: OperatorInvoiceFilters;
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

  // The canonical query of the view being shown: the fetch key below, so one
  // string decides both what is requested and when it is re-requested.
  const viewQuery = operatorInvoiceListQuery(page, filters);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // The count is read beside the page: it says whether Sale Invoicing is
      // open (404 while closed, read as "no filter") and how many the filter
      // would find. A closed feature must not take the list down with it.
      const [listPage, warningCount, uninvoiced] = await Promise.all([
        fetchOperatorInvoiceList(page, filters),
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
      router.push(`${LIST_PATH}${operatorInvoiceListQueryAfterFilterChange(filters, patch)}`);
    },
    [filters, router],
  );

  const goToPage = useCallback(
    (next: number) => {
      const target = Math.max(1, next);
      if (target === page) {
        return;
      }
      // Paging keeps the filters: only the page moves.
      router.push(`${LIST_PATH}${operatorInvoiceListQuery(target, filters)}`);
    },
    [filters, page, router],
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
          <CardHeader className="flex flex-row items-start justify-between gap-4">
            <div>
              <CardTitle>{t("invoicingListTitle")}</CardTitle>
              <CardDescription>{t("invoicingListDescription")}</CardDescription>
            </div>
            <div className="flex flex-col items-end gap-2">
              <InvoiceSearchBox q={filters.q} onApply={applyFilters} />
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">{t("invoicingKindFilterLabel")}</span>
                <select
                  className={SELECT_CLASS}
                  value={filters.kind}
                  onChange={(event) => applyFilters({ kind: event.target.value as InvoiceKindFilter })}
                >
                  {INVOICE_KIND_FILTERS.map((option) => (
                    <option key={option} value={option}>
                      {option === "all" ? t("invoicingKindFilterAll") : t(INVOICE_KIND_KEYS[option as InvoiceKind])}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">{t("invoicingStatusFilterLabel")}</span>
                <select
                  className={SELECT_CLASS}
                  value={filters.status}
                  onChange={(event) => applyFilters({ status: event.target.value as InvoiceStatusFilter })}
                >
                  {INVOICE_STATUS_FILTERS.map((option) => (
                    <option key={option} value={option}>
                      {option === "all" ? t("invoicingStatusFilterAll") : t(INVOICE_STATUS_KEYS[option])}
                    </option>
                  ))}
                </select>
              </label>
              {recipientWarningCount !== null ? (
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="h-4 w-4"
                    checked={filters.recipientWarningOnly}
                    onChange={(event) => applyFilters({ recipientWarningOnly: event.target.checked })}
                  />
                  <span>{t("invoicingRecipientWarningFilter", { count: recipientWarningCount })}</span>
                </label>
              ) : null}
              {uninvoicedCount !== null ? (
                <Link href="/operator/invoicing/uninvoiced" className="text-sm hover:underline">
                  {t("invoicingUninvoicedCountLink", { count: uninvoicedCount })}
                </Link>
              ) : null}
            </div>
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
              // sentence about the Kind.
              <p className="text-sm text-muted-foreground">
                {!hasActiveOperatorInvoiceFilters(filters)
                  ? t("invoicingListEmpty")
                  : filters.q
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
                    <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                      <th className="py-2 pr-4 font-medium">{t("invoicingColNumber")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColKind")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColSale")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColDate")}</th>
                      <th className="py-2 pr-4 font-medium">{t("invoicingColRecipient")}</th>
                      <th className="py-2 pr-4 text-right font-medium">{t("invoicingColTotal")}</th>
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
