"use client";

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

import {
  ApiError,
  fetchEventsJSON,
  getPublishMissingFields,
  missingFieldsFromDetails,
  PUBLISH_FIELD_LABELS,
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

function fieldLabel(field: string): string {
  return PUBLISH_FIELD_LABELS[field] ?? field;
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
      toast.success("Event published");
      router.refresh();
    } catch (publishError) {
      toast.error(publishError instanceof Error ? publishError.message : "Failed to publish event");
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
      toast.success("Event cancelled");
      router.refresh();
    } catch (cancelError) {
      toast.error(cancelError instanceof Error ? cancelError.message : "Failed to cancel event");
    } finally {
      setCancelling(false);
    }
  }

  async function handleDelete() {
    try {
      await fetchEventsJSON<{ message: string }>(`/api/events/${eventId}`, {
        method: "DELETE",
      });
      toast.success("Event deleted");
      router.push("/events");
      router.refresh();
    } catch (deleteError) {
      toast.error(deleteError instanceof Error ? deleteError.message : "Failed to delete event");
    }
  }

  const publishHint =
    status === "draft" && !canPublish
      ? `Save the missing details to publish: ${missingFields.map(fieldLabel).join(", ")}.`
      : null;

  return (
    <div className="rounded-lg border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate text-lg font-semibold">{name || "Event"}</span>
          <Badge variant={statusBadgeVariant(status)} className="shrink-0 capitalize">
            {status}
          </Badge>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {status === "draft" ? (
            <>
              <Button type="button" disabled={!canPublish || publishing} onClick={() => void handlePublish()}>
                {publishing ? "Publishing..." : "Publish event"}
              </Button>
              <Button type="button" variant="ghost" onClick={() => setDeleteOpen(true)}>
                Delete
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
                Cancel event
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
            <span>Registration link:</span>
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
              <span>not added yet</span>
            )}
          </p>
          <p>
            <span className="font-medium text-foreground">
              {registrationClickCount.toLocaleString()}{" "}
              {registrationClickCount === 1 ? "click" : "clicks"}
            </span>{" "}
            from this event&apos;s page. Clicks, not registrations — what happens on the other site
            is not visible here.
          </p>
        </div>
      ) : null}

      <Dialog open={cancelOpen} onOpenChange={setCancelOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Cancel published event?</DialogTitle>
            <DialogDescription>
              This marks the event as cancelled. It stays in your catalog with a stable record, but it is no longer
              active.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setCancelOpen(false)}>
              Keep published
            </Button>
            <Button type="button" variant="destructive" disabled={cancelling} onClick={() => void handleCancel()}>
              {cancelling ? "Cancelling..." : "Cancel event"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete draft event?</DialogTitle>
            <DialogDescription>
              This permanently removes the event and any assignments. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button type="button" variant="destructive" onClick={() => void handleDelete()}>
              Delete event
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
