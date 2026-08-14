"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { FormEvent, useCallback, useEffect, useState } from "react";

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
      // The URL is on screen and selectable, so say so rather than fail mutely.
      toast.error(t("copyFailed"));
    }
  }

  // Whether this Event's links are measured in sales at all, read off the rows
  // themselves: the API suppresses both attribution figures on an Event that
  // registers externally, and that absence is the only signal this section needs
  // — no second request, and no second copy of the Event's registration mode to
  // fall out of step with the figures it explains. With no links yet there is
  // nothing to explain either way, and the ordinary wording stands.
  const attributionMeasured = links.length === 0 || links.some((link) => link.sales_count !== null);

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

        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, index) => (
              <Skeleton key={index} className="h-24 w-full" />
            ))}
          </div>
        ) : loadError ? (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            <AlertDescription>{loadError}</AlertDescription>
          </Alert>
        ) : links.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("empty")}</p>
        ) : (
          <div className="space-y-3">
            {links.map((link) => (
              <div
                key={link.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="min-w-0">
                  <p className="font-medium">{link.name}</p>
                  <p className="break-all text-sm text-muted-foreground">{link.url}</p>
                  {/* What the link has actually done: active attributed sales,
                      and what they left the Organization. A reversed sale is in
                      neither. Both are informational — no commission is owed on
                      either figure.

                      On an Event that registers externally the API reports
                      neither, and the whole line goes with them: a link there
                      can never attribute a sale, so a "0 sales · $0.00 net
                      proceeds" would read as a failed link rather than one whose
                      success is measured in clicks. Absent, not zeroed, and not
                      an em dash either — a dash is still a claim that something
                      is missing. The clicks beside it are the figure that
                      means something. */}
                  {link.sales_count !== null && link.net_proceeds_cents !== null ? (
                    <p className="mt-1 text-sm text-muted-foreground">
                      {t.rich("attribution", {
                        count: link.sales_count,
                        // The Event's currency, whichever language is read. An
                        // em dash while the currency is still unknown: a bare
                        // number would read as a figure in whatever currency
                        // the reader assumed.
                        amount: currency
                          ? formatMoney(link.net_proceeds_cents, currency, locale)
                          : "—",
                        value: (chunks) => (
                          <span className="font-medium text-foreground">{chunks}</span>
                        ),
                      })}
                    </p>
                  ) : null}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {/* Clicks sit next to the link so a bad link reads differently
                      from a bad audience: no clicks means nobody followed it. */}
                  <span className="text-sm text-muted-foreground">
                    {t.rich("clicks", {
                      count: link.clicks,
                      value: (chunks) => (
                        <span className="font-medium text-foreground">{chunks}</span>
                      ),
                    })}
                  </span>
                  <Badge variant={link.active ? "default" : "secondary"}>
                    {link.active ? t("active") : t("inactive")}
                  </Badge>
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
              </div>
            ))}
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
