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

type EventHeaderBarProps = {
  eventId: string;
  name: string;
  status: string;
  /** Persisted Event fields + Ticket Type count that drive publish-readiness. */
  slug: string;
  startsAt: string | null;
  timezone: string | null;
  ticketTypeCount: number;
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
}: EventHeaderBarProps) {
  const router = useRouter();
  const [publishing, setPublishing] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [backendMissingFields, setBackendMissingFields] = useState<string[] | null>(null);

  // Readiness is a pure function of SAVED server state (persisted Event fields
  // fetched by the layout + persisted Ticket Type count) — never form state.
  const missingFields =
    backendMissingFields ??
    getPublishMissingFields({ name, slug, starts_at: startsAt, timezone }, ticketTypeCount);
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
            <Button type="button" variant="destructive" onClick={() => setCancelOpen(true)}>
              Cancel event
            </Button>
          ) : null}
        </div>
      </div>
      {publishHint ? <p className="mt-2 text-sm text-muted-foreground">{publishHint}</p> : null}

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
