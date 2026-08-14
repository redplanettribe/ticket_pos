"use client";

import { useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useState } from "react";

import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import {
  ApiError,
  eventStatusKey,
  fetchEventsJSON,
  getPublishMissingFields,
  isPublishFieldKey,
  missingFieldsFromDetails,
  statusBadgeVariant,
  type EventDetail,
} from "@/lib/events-api";
import type { RegistrationMode } from "@/lib/registration";

import { DiscoverabilityToggle } from "../discoverability-toggle";

type EventHeaderBarProps = {
  eventId: string;
  name: string;
  status: string;
  /** Persisted Event fields + Ticket Type count that drive publish-readiness. */
  slug: string;
  startsAt: string | null;
  timezone: string | null;
  ticketTypeCount: number;
  /** The registration pair: which way in the Event needs before it can publish. */
  registrationMode: RegistrationMode;
  registrationUrl: string | null;
  /**
   * Hand-offs to the Registration Link, shown beside it — a single integer does
   * not earn a page of its own. It sits here rather than in the Details form
   * because it is not sensitive and every Member of the Event may read it,
   * including Event Staff, who never reach that form (page.tsx sends them to
   * the area they manage). Gating it more tightly than the Sales list it stands
   * in for on such an Event would be strange.
   */
  registrationClickCount: number;
  /** Seeds the Discoverable toggle; the same flag the events list edits. */
  discoverable: boolean;
};

/**
 * The `event` catalog key a missing publish requirement is named by —
 * "starts_at" becomes `publishFieldStartsAt`. Derived rather than mapped so a
 * key added to the mirror is a compile error at the catalog rather than a
 * silently dropped requirement.
 */
function publishFieldKey(field: string) {
  return `publishField${field
    .split("_")
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join("")}` as
    | "publishFieldName"
    | "publishFieldSlug"
    | "publishFieldStartsAt"
    | "publishFieldTimezone"
    | "publishFieldTicketTypes"
    | "publishFieldRegistrationUrl";
}

export function EventHeaderBar({
  eventId,
  name,
  status,
  slug,
  startsAt,
  timezone,
  ticketTypeCount,
  registrationMode,
  registrationUrl,
  registrationClickCount,
  discoverable: initialDiscoverable,
}: EventHeaderBarProps) {
  const t = useTranslations("event");
  // The Event status vocabulary belongs to the events surface, which coined it;
  // this surface reads it rather than saying "Published" a second way.
  const events = useTranslations("events");
  const errorCopy = useMessages().errors;
  const router = useRouter();
  // The toggle's own endpoint is idempotent and returns the updated Event, so
  // its response is authoritative and nothing else on the page derives from the
  // flag — local state, no router.refresh().
  const [discoverable, setDiscoverable] = useState(initialDiscoverable);
  const [publishing, setPublishing] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [backendMissingFields, setBackendMissingFields] = useState<string[] | null>(null);

  // Readiness is a pure function of SAVED server state (persisted Event fields
  // fetched by the layout + persisted Ticket Type count) — never form state.
  const missingFields =
    backendMissingFields ??
    getPublishMissingFields(
      {
        name,
        slug,
        starts_at: startsAt,
        timezone,
        registration_mode: registrationMode,
        registration_url: registrationUrl,
      },
      ticketTypeCount,
    );
  const canPublish = status === "draft" && missingFields.length === 0;

  async function handlePublish() {
    setPublishing(true);
    setBackendMissingFields(null);
    try {
      await fetchEventsJSON<EventDetail>(`/api/events/${eventId}/publish`, {
        method: "POST",
      });
      toast.success(t("publishedToast"));
      router.refresh();
    } catch (publishError) {
      toast.error(
        apiErrorMessage(errorCopy, publishError instanceof ApiError ? publishError : null) ??
          t("publishFailed"),
      );
      if (publishError instanceof ApiError) {
        setBackendMissingFields(missingFieldsFromDetails(publishError.details));
      }
    } finally {
      setPublishing(false);
    }
  }

  async function handleCancel() {
    setCancelling(true);
    try {
      await fetchEventsJSON<EventDetail>(`/api/events/${eventId}/cancel`, {
        method: "POST",
      });
      setCancelOpen(false);
      toast.success(t("cancelledToast"));
      router.refresh();
    } catch (cancelError) {
      toast.error(
        apiErrorMessage(errorCopy, cancelError instanceof ApiError ? cancelError : null) ??
          t("cancelFailed"),
      );
    } finally {
      setCancelling(false);
    }
  }

  async function handleDelete() {
    try {
      await fetchEventsJSON<{ message: string }>(`/api/events/${eventId}`, {
        method: "DELETE",
      });
      toast.success(t("deletedToast"));
      router.push("/events");
      router.refresh();
    } catch (deleteError) {
      toast.error(
        apiErrorMessage(errorCopy, deleteError instanceof ApiError ? deleteError : null) ??
          t("deleteFailed"),
      );
    }
  }

  // The list is joined here rather than in the catalog because ICU has no list
  // formatter and a comma is the same mark in both languages; the SENTENCE
  // around it is the catalog's, so Spanish is free to put the list elsewhere in
  // it. A key the server names and this app has no word for is shown raw.
  const publishHint =
    status === "draft" && !canPublish
      ? t("publishHint", {
          fields: missingFields
            .map((field) => (isPublishFieldKey(field) ? t(publishFieldKey(field)) : field))
            .join(", "),
        })
      : null;

  const statusKey = eventStatusKey(status);

  return (
    <div className="rounded-lg border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate text-lg font-semibold">{name || t("fallbackName")}</span>
          <Badge variant={statusBadgeVariant(status)} className="shrink-0">
            {statusKey ? events(statusKey) : status}
          </Badge>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {status === "draft" ? (
            <>
              <Button type="button" disabled={!canPublish || publishing} onClick={() => void handlePublish()}>
                {publishing ? t("publishing") : t("publish")}
              </Button>
              <Button type="button" variant="ghost" onClick={() => setDeleteOpen(true)}>
                {t("delete")}
              </Button>
            </>
          ) : null}
          {status === "published" ? (
            <>
              <DiscoverabilityToggle
                eventId={eventId}
                status={status}
                discoverable={discoverable}
                onChange={setDiscoverable}
              />
              <Button type="button" variant="destructive" onClick={() => setCancelOpen(true)}>
                {t("cancelEvent")}
              </Button>
            </>
          ) : null}
        </div>
      </div>
      {publishHint ? <p className="mt-2 text-sm text-muted-foreground">{publishHint}</p> : null}

      {/* An externally registered Event's one measurable: how many people this
          page handed to the registration site. Shown beside the link itself,
          because the number means nothing without the destination it counts.

          The word is CLICKS and may never become "registrations" or "people".
          The platform loses sight of the visitor at the link and never learns
          whether they signed up, nor whether the same person came back three
          times — only the site on the other side knows either. The counter is
          deliberately not deduplicated, so it counts clicks and says so. */}
      {registrationMode === "external" ? (
        <div className="mt-2 space-y-1 text-sm text-muted-foreground">
          <p className="flex flex-wrap items-center gap-x-2">
            <span>{t("registrationLinkLabel")}</span>
            {registrationUrl ? (
              <a
                href={registrationUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="min-w-0 truncate underline underline-offset-2"
              >
                {registrationUrl}
              </a>
            ) : (
              <span>{t("registrationLinkMissing")}</span>
            )}
          </p>
          {/* One ICU message rather than a count glued to a noun: "1 click" and
              "3 clics" do not inflect the same way, and the whole sentence is
              the translator's to arrange. ICU draws the number in the Staff
              Locale, which is what replaces the `toLocaleString()` that used to
              follow the reader's browser instead (ADR 0041). */}
          <p>
            {t.rich("registrationClicks", {
              count: registrationClickCount,
              value: (chunks) => <span className="font-medium text-foreground">{chunks}</span>,
            })}
          </p>
        </div>
      ) : null}

      <Dialog open={cancelOpen} onOpenChange={setCancelOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("cancelDialogTitle")}</DialogTitle>
            <DialogDescription>{t("cancelDialogDescription")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setCancelOpen(false)}>
              {t("keepPublished")}
            </Button>
            <Button type="button" variant="destructive" disabled={cancelling} onClick={() => void handleCancel()}>
              {cancelling ? t("cancelling") : t("cancelEvent")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("deleteDialogTitle")}</DialogTitle>
            <DialogDescription>{t("deleteDialogDescription")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteOpen(false)}>
              {t("cancel")}
            </Button>
            <Button type="button" variant="destructive" onClick={() => void handleDelete()}>
              {t("deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
