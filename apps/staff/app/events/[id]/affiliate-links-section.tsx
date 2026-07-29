"use client";

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
import { formatPriceCents } from "@/lib/events-api";
import { fetchSalesSummary } from "@/lib/sales-api";

type AffiliateLinksSectionProps = {
  eventId: string;
};

export function AffiliateLinksSection({ eventId }: AffiliateLinksSectionProps) {
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
  // formatted the way the summary strip formats it (formatPriceCents) and shown
  // as an em dash until the currency is known: a bare number would read as a
  // figure in whatever currency the reader assumed.
  const [currency, setCurrency] = useState<string | null>(null);

  const loadLinks = useCallback(async () => {
    setLoading(true);
    try {
      setLinks(await listAffiliateLinks(eventId));
      setLoadError(null);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Failed to load affiliate links");
    } finally {
      setLoading(false);
    }
  }, [eventId]);

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
      toast.success("Affiliate link created");
    } catch (createError) {
      toast.error(createError instanceof Error ? createError.message : "Failed to create affiliate link");
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
      toast.success("Affiliate link renamed");
    } catch (renameError) {
      toast.error(renameError instanceof Error ? renameError.message : "Failed to rename affiliate link");
    } finally {
      setBusyId(null);
    }
  }

  async function toggleActive(link: AffiliateLink) {
    setBusyId(link.id);
    try {
      await updateAffiliateLink(eventId, link.id, { active: !link.active });
      await loadLinks();
      toast.success(link.active ? "Affiliate link deactivated" : "Affiliate link reactivated");
    } catch (toggleError) {
      toast.error(
        toggleError instanceof Error ? toggleError.message : "Failed to update affiliate link",
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
      toast.success("Affiliate link deleted");
    } catch (deleteError) {
      // The API refuses a link with any history, and its message says to
      // deactivate instead — surfaced as it comes rather than restated here.
      toast.error(deleteError instanceof Error ? deleteError.message : "Failed to delete affiliate link");
    } finally {
      setBusyId(null);
    }
  }

  async function copyURL(link: AffiliateLink) {
    try {
      await navigator.clipboard.writeText(link.url);
      setCopiedId(link.id);
      window.setTimeout(() => setCopiedId((current) => (current === link.id ? null : current)), 2000);
      toast.success("Link copied");
    } catch {
      // Clipboard access can be refused (insecure origin, denied permission).
      // The URL is on screen and selectable, so say so rather than fail mutely.
      toast.error("Could not copy — select the link and copy it manually");
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Affiliate Links</CardTitle>
        <CardDescription>
          Named links to this Event&apos;s page that attribute Online Sales to whoever is promoting it. The
          code is generated for you and never changes.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <form className="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={(event) => void handleCreate(event)}>
          <div className="flex-1">
            <FormField id="affiliate-link-name" label="Name">
              <Input
                id="affiliate-link-name"
                value={name}
                maxLength={AFFILIATE_LINK_NAME_MAX_LENGTH}
                onChange={(event) => setName(event.target.value)}
                placeholder="María's Instagram"
                required
              />
            </FormField>
          </div>
          <Button
            type="submit"
            disabled={creating || name.trim() === ""}
            aria-busy={creating}
          >
            {creating ? "Creating…" : "Create affiliate link"}
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
            <AlertTitle>Could not load affiliate links</AlertTitle>
            <AlertDescription>{loadError}</AlertDescription>
          </Alert>
        ) : links.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No affiliate links yet. Create one to give a promoter their own link to this Event.
          </p>
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
                      either figure. */}
                  <p className="mt-1 text-sm text-muted-foreground">
                    <span className="font-medium text-foreground">{link.sales_count}</span>{" "}
                    {link.sales_count === 1 ? "sale" : "sales"}
                    {" · "}
                    <span className="font-medium text-foreground">
                      {currency ? formatPriceCents(link.net_proceeds_cents, currency) : "—"}
                    </span>{" "}
                    net proceeds
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {/* Clicks sit next to the link so a bad link reads differently
                      from a bad audience: no clicks means nobody followed it. */}
                  <span className="text-sm text-muted-foreground">
                    <span className="font-medium text-foreground">{link.clicks}</span>{" "}
                    {link.clicks === 1 ? "click" : "clicks"}
                  </span>
                  <Badge variant={link.active ? "default" : "secondary"}>
                    {link.active ? "Active" : "Inactive"}
                  </Badge>
                  <Button type="button" variant="outline" size="sm" onClick={() => void copyURL(link)}>
                    {copiedId === link.id ? "Copied" : "Copy link"}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={busyId === link.id}
                    aria-busy={busyId === link.id}
                    onClick={() => openRenameDialog(link)}
                  >
                    Rename
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
                        ? "Deactivating…"
                        : "Reactivating…"
                      : link.active
                        ? "Deactivate"
                        : "Reactivate"}
                  </Button>
                  <Button
                    type="button"
                    variant="destructive"
                    size="sm"
                    disabled={busyId === link.id}
                    aria-busy={busyId === link.id}
                    onClick={() => setDeleteTarget(link)}
                  >
                    Delete
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
            <DialogTitle>Rename affiliate link</DialogTitle>
            <DialogDescription>
              Only the name changes. The code, the link itself, and everything it has already done stay
              exactly as they are.
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={(event) => void handleRename(event)}>
            <FormField id="affiliate-link-rename" label="Name">
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
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={busyId !== null || renameValue.trim() === ""}
                aria-busy={busyId !== null}
              >
                {busyId !== null ? "Saving…" : "Save name"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete affiliate link?</DialogTitle>
            <DialogDescription>
              Remove <strong>{deleteTarget?.name}</strong> from this Event? This cannot be undone. A link
              that has any clicks or attributed sales cannot be deleted — deactivate it instead, and it
              keeps its history.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={busyId !== null}
              aria-busy={busyId !== null}
              onClick={() => void handleDelete()}
            >
              {busyId !== null ? "Deleting…" : "Delete affiliate link"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
