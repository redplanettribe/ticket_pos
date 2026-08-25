"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

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
  FormField,
  Input,
  Skeleton,
  toast,
} from "@ticket-pos/ui";

import {
  AFFILIATE_LINK_NAME_MAX_LENGTH,
  createAffiliateLink,
  deleteAffiliateLink,
  listAffiliateLinks,
  updateAffiliateLink,
  type AffiliateLink,
} from "@/lib/affiliates-api";
import {
  DEFAULT_AFFILIATE_SORT,
  filterAffiliateLinks,
  isAttributionMeasured,
  nextAffiliateSort,
  resolveAffiliateSort,
  sortAffiliateLinks,
  type AffiliateSort,
  type AffiliateSortField,
} from "@/lib/affiliate-links-view";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatMoney } from "@/lib/format";
import { fetchSalesSummary } from "@/lib/sales-api";

type AffiliateLinksSectionProps = {
  eventId: string;
};

export function AffiliateLinksSection({ eventId }: AffiliateLinksSectionProps) {
  const t = useTranslations("affiliateLinks");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [loading, setLoading] = useState(true);
  // Why the section itself is empty, when it is. A section that will not load is
  // a page-level failure and gets the banner every other one gets — toasts are
  // for the mutations below, which leave the list on screen behind them.
  const [loadError, setLoadError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [links, setLinks] = useState<AffiliateLink[]>([]);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  // The table order. Component state, not URL state: the default view is what
  // every fresh visit should show, and keeping it here is what lets a rename,
  // toggle or delete reload the list without throwing the chosen order away.
  const [sort, setSort] = useState<AffiliateSort>(DEFAULT_AFFILIATE_SORT);
  // The search box's text, kept here for the same reason as the sort: a
  // rename, toggle or delete reloads the list and the query stays put.
  const [query, setQuery] = useState("");
  // The two lifecycle dialogs, each holding the row it was opened on. A rename
  // is an edit, a delete is destructive and confirmed the way every other
  // destructive staff action is; the activate/deactivate toggle is reversible
  // and asks nothing.
  const [renameTarget, setRenameTarget] = useState<AffiliateLink | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<AffiliateLink | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  // The Event's currency, for the attributed Net Proceeds figures. It is read
  // from the sales summary — the same figure's own surface, behind the same
  // Org-Admin/Event-Owner gate — rather than kept a second time here. Money is
  // formatted the way the summary strip formats it (formatMoney) and shown
  // as an em dash until the currency is known: a bare number would read as a
  // figure in whatever currency the reader assumed.
  const [currency, setCurrency] = useState<string | null>(null);

  const loadLinks = useCallback(async () => {
    setLoading(true);
    try {
      setLinks(await listAffiliateLinks(eventId));
      setLoadError(null);
    } catch (error) {
      setLoadError(
        apiErrorMessage(errorCopy, error instanceof ApiError ? error : null) ?? t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [errorCopy, eventId, t]);

  useEffect(() => {
    void loadLinks();
  }, [loadLinks]);

  useEffect(() => {
    let cancelled = false;
    fetchSalesSummary(eventId)
      .then((summary) => {
        if (!cancelled) {
          setCurrency(summary.currency);
        }
      })
      .catch(() => {
        // Silent: the attributed sale counts are the point of this section, and
        // a missing currency label is not worth a second error toast over the
        // one loadLinks already raises when the section itself fails.
      });
    return () => {
      cancelled = true;
    };
  }, [eventId]);

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      return;
    }
    setCreating(true);
    try {
      await createAffiliateLink(eventId, trimmed);
      setName("");
      await loadLinks();
      toast.success(t("createdToast"));
    } catch (createError) {
      toast.error(
        apiErrorMessage(errorCopy, createError instanceof ApiError ? createError : null) ??
          t("createFailed"),
      );
    } finally {
      setCreating(false);
    }
  }

  function openRenameDialog(link: AffiliateLink) {
    setRenameTarget(link);
    setRenameValue(link.name);
  }

  async function handleRename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!renameTarget) {
      return;
    }
    const trimmed = renameValue.trim();
    if (!trimmed) {
      return;
    }
    setBusyId(renameTarget.id);
    try {
      await updateAffiliateLink(eventId, renameTarget.id, { name: trimmed });
      setRenameTarget(null);
      await loadLinks();
      toast.success(t("renamedToast"));
    } catch (renameError) {
      toast.error(
        apiErrorMessage(errorCopy, renameError instanceof ApiError ? renameError : null) ??
          t("renameFailed"),
      );
    } finally {
      setBusyId(null);
    }
  }

  async function toggleActive(link: AffiliateLink) {
    setBusyId(link.id);
    try {
      await updateAffiliateLink(eventId, link.id, { active: !link.active });
      await loadLinks();
      toast.success(link.active ? t("deactivatedToast") : t("reactivatedToast"));
    } catch (toggleError) {
      toast.error(
        apiErrorMessage(errorCopy, toggleError instanceof ApiError ? toggleError : null) ??
          t("updateFailed"),
      );
    } finally {
      setBusyId(null);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) {
      return;
    }
    setBusyId(deleteTarget.id);
    try {
      await deleteAffiliateLink(eventId, deleteTarget.id);
      setDeleteTarget(null);
      await loadLinks();
      toast.success(t("deletedToast"));
    } catch (deleteError) {
      // The API refuses a link with any history, and AFFILIATE_LINK_HAS_HISTORY
      // is keyed in `errors.envelope` so that refusal — the one an organizer is
      // most likely to meet here — arrives in their own language.
      toast.error(
        apiErrorMessage(errorCopy, deleteError instanceof ApiError ? deleteError : null) ??
          t("deleteFailed"),
      );
    } finally {
      setBusyId(null);
    }
  }

  async function copyURL(link: AffiliateLink) {
    try {
      await navigator.clipboard.writeText(link.url);
      setCopiedId(link.id);
      window.setTimeout(() => setCopiedId((current) => (current === link.id ? null : current)), 2000);
      toast.success(t("copiedToast"));
    } catch {
      // Clipboard access can be refused (insecure origin, denied permission).
      // Say so rather than fail mutely.
      toast.error(t("copyFailed"));
    }
  }

  // Whether this Event's links are measured in sales at all, read off the rows
  // themselves: the API suppresses both attribution figures on an Event that
  // registers externally, and that absence is the only signal this section needs
  // — no second request, and no second copy of the Event's registration mode to
  // fall out of step with the figures it explains. With no links yet there is
  // nothing to explain either way, and the ordinary wording stands.
  const attributionMeasured = isAttributionMeasured(links);

  // The sort actually applied. The chosen one is kept as chosen — a Sales sort
  // picked before the list turned out unmeasured is not thrown away, only set
  // aside for Clicks desc while the column it names is absent.
  const activeSort = resolveAffiliateSort(sort, attributionMeasured);
  const sortedLinks = useMemo(
    () => sortAffiliateLinks(links, activeSort.field, activeSort.dir),
    [links, activeSort.field, activeSort.dir],
  );
  // The rows on screen: the sorted list narrowed by the search. Filter after
  // sort so the order is the sort's whatever the query.
  const visibleLinks = useMemo(() => filterAffiliateLinks(sortedLinks, query), [sortedLinks, query]);

  function toggleSort(field: AffiliateSortField) {
    setSort((current) => nextAffiliateSort(resolveAffiliateSort(current, attributionMeasured), field));
  }

  // The table's columns, in order. Sales and Net proceeds are omitted — not
  // blanked — on an Event that registers externally: a link there can never
  // attribute a sale, so a "0" and "$0.00" would read as a failed link rather
  // than one whose success is measured in clicks. Absent, not zeroed, and not
  // an em dash either — a dash is still a claim that something is missing.
  // Whatever is declared here is what the header, the skeleton and the body
  // agree on; a column without a `sortField` is a plain header.
  const columns: Column[] = [
    { key: "name", label: t("colName"), sortField: "name" },
    { key: "code", label: t("colCode") },
    { key: "clicks", label: t("colClicks"), sortField: "clicks", numeric: true },
    ...(attributionMeasured
      ? ([
          { key: "sales", label: t("colSales"), sortField: "sales", numeric: true },
          { key: "net_proceeds", label: t("colNetProceeds"), sortField: "net_proceeds", numeric: true },
        ] satisfies Column[])
      : []),
    { key: "status", label: t("colStatus") },
    { key: "actions", label: t("colActions") },
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("title")}</CardTitle>
        <CardDescription>
          {/* The promise the card makes has to be one this Event can keep. An
              Event that registers externally never attributes a sale, and the
              API says so by reporting no attribution figures at all — so the
              description drops the claim rather than leaving it standing over
              rows that will never show it. */}
          {attributionMeasured ? t("descriptionAttributed") : t("descriptionClicksOnly")}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <form className="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={(event) => void handleCreate(event)}>
          <div className="flex-1">
            <FormField id="affiliate-link-name" label={t("nameLabel")}>
              <Input
                id="affiliate-link-name"
                value={name}
                maxLength={AFFILIATE_LINK_NAME_MAX_LENGTH}
                onChange={(event) => setName(event.target.value)}
                placeholder={t("namePlaceholder")}
                required
              />
            </FormField>
          </div>
          <Button
            type="submit"
            disabled={creating || name.trim() === ""}
            aria-busy={creating}
          >
            {creating ? t("creating") : t("create")}
          </Button>
        </form>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Input
            type="search"
            className="sm:flex-1"
            placeholder={t("searchPlaceholder")}
            aria-label={t("searchPlaceholder")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>

        {loading ? (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <tbody>
                {Array.from({ length: 3 }).map((_, index) => (
                  <tr key={index} className="border-b">
                    {columns.map((column) => (
                      <td key={column.key} className="py-3 pr-4">
                        <Skeleton className="h-4 w-full" />
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : loadError ? (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            <AlertDescription>{loadError}</AlertDescription>
          </Alert>
        ) : links.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("empty")}</p>
        ) : visibleLinks.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("noMatches")}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                  {columns.map((column) =>
                    column.sortField ? (
                      <SortableHeader
                        key={column.key}
                        label={column.label}
                        field={column.sortField}
                        numeric={column.numeric}
                        sort={activeSort}
                        onSort={toggleSort}
                        sortedAscending={t("sortedAscending")}
                        sortedDescending={t("sortedDescending")}
                      />
                    ) : (
                      <th key={column.key} className="py-2 pr-4 font-medium">
                        {column.label}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {visibleLinks.map((link) => (
                  <tr key={link.id} className="border-b last:border-b-0">
                    <td className="py-3 pr-4 font-medium">{link.name}</td>
                    {/* The code, never the whole URL: the URL is identical on
                        every row but for this, so the code is what tells rows
                        apart. Copy link still hands over the full URL. */}
                    <td className="py-3 pr-4 font-mono text-xs">{link.code}</td>
                    {/* Clicks sit on every Event so a bad link reads differently
                        from a bad audience: no clicks means nobody followed it. */}
                    <td className="py-3 pr-4 text-right tabular-nums">{link.clicks}</td>
                    {/* What the link has actually done: active attributed sales,
                        and what they left the Organization. A reversed sale is in
                        neither. Both are informational — no commission is owed on
                        either figure. Net proceeds is the Event's currency,
                        whichever language is read, and an em dash while the
                        currency is still unknown: a bare number would read as a
                        figure in whatever currency the reader assumed. */}
                    {attributionMeasured ? (
                      <>
                        <td className="py-3 pr-4 text-right tabular-nums">{link.sales_count ?? ""}</td>
                        <td className="py-3 pr-4 text-right tabular-nums whitespace-nowrap">
                          {link.net_proceeds_cents !== null
                            ? currency
                              ? formatMoney(link.net_proceeds_cents, currency, locale)
                              : "—"
                            : ""}
                        </td>
                      </>
                    ) : null}
                    <td className="py-3 pr-4">
                      <Badge variant={link.active ? "default" : "secondary"}>
                        {link.active ? t("active") : t("inactive")}
                      </Badge>
                    </td>
                    <td className="py-3 pr-4">
                      <div className="flex flex-wrap items-center gap-2">
                        <Button type="button" variant="outline" size="sm" onClick={() => void copyURL(link)}>
                          {copiedId === link.id ? t("copied") : t("copy")}
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={busyId === link.id}
                          aria-busy={busyId === link.id}
                          onClick={() => openRenameDialog(link)}
                        >
                          {t("rename")}
                        </Button>
                        {/* Deactivating leaves everything on this row where it is and
                            only stops the code counting and attributing; reactivating
                            resumes both under the same link. */}
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={busyId === link.id}
                          aria-busy={busyId === link.id}
                          onClick={() => void toggleActive(link)}
                        >
                          {busyId === link.id
                            ? link.active
                              ? t("deactivating")
                              : t("reactivating")
                            : link.active
                              ? t("deactivate")
                              : t("reactivate")}
                        </Button>
                        <Button
                          type="button"
                          variant="destructive"
                          size="sm"
                          disabled={busyId === link.id}
                          aria-busy={busyId === link.id}
                          onClick={() => setDeleteTarget(link)}
                        >
                          {t("delete")}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>

      <Dialog open={renameTarget !== null} onOpenChange={(open) => !open && setRenameTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("renameDialogTitle")}</DialogTitle>
            <DialogDescription>{t("renameDialogDescription")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void handleRename(event)}>
            <FormField id="affiliate-link-rename" label={t("nameLabel")}>
              <Input
                id="affiliate-link-rename"
                value={renameValue}
                maxLength={AFFILIATE_LINK_NAME_MAX_LENGTH}
                onChange={(event) => setRenameValue(event.target.value)}
                required
              />
            </FormField>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setRenameTarget(null)}>
                {t("cancel")}
              </Button>
              <Button
                type="submit"
                disabled={busyId !== null || renameValue.trim() === ""}
                aria-busy={busyId !== null}
              >
                {busyId !== null ? t("saving") : t("saveName")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("deleteDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t.rich("deleteDialogDescription", {
                name: deleteTarget?.name ?? "",
                em: (chunks) => <strong>{chunks}</strong>,
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteTarget(null)}>
              {t("cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={busyId !== null}
              aria-busy={busyId !== null}
              onClick={() => void handleDelete()}
            >
              {busyId !== null ? t("deleting") : t("deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

// A column of the Affiliate Links table. Declared as data so the header, the
// loading skeleton and the body agree on the set; `sortField` makes the header
// a sort button, `numeric` right-aligns it over its right-aligned cells.
type Column = {
  key: string;
  label: string;
  sortField?: AffiliateSortField;
  numeric?: boolean;
};

type SortableHeaderProps = {
  label: string;
  field: AffiliateSortField;
  numeric?: boolean;
  sort: AffiliateSort;
  onSort: (field: AffiliateSortField) => void;
  sortedAscending: string;
  sortedDescending: string;
};

// SortableHeader is a column header that toggles the table sort, the Sales
// list's header over again. The active column shows a direction arrow; clicking
// flips it, clicking another column switches to it in its natural direction.
// aria-sort exposes the state to assistive tech, and the title names it for a
// pointer resting on the arrow.
function SortableHeader({
  label,
  field,
  numeric,
  sort,
  onSort,
  sortedAscending,
  sortedDescending,
}: SortableHeaderProps) {
  const active = sort.field === field;
  return (
    <th
      className={numeric ? "py-2 pr-4 text-right font-medium" : "py-2 pr-4 font-medium"}
      aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(field)}
        title={active ? (sort.dir === "asc" ? sortedAscending : sortedDescending) : undefined}
        className="inline-flex items-center gap-1 uppercase tracking-wide hover:text-foreground"
      >
        {label}
        <span aria-hidden className={active ? "text-foreground" : "text-muted-foreground/40"}>
          {active ? (sort.dir === "asc" ? "▲" : "▼") : "↕"}
        </span>
      </button>
    </th>
  );
}
