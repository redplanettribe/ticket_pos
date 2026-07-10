"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
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
  PageHeader,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import {
  COMMON_TIMEZONES,
  dateTimeLocalToISO,
  fetchEventsJSON,
  getPublishMissingFields,
  isoToDateTimeLocal,
  PUBLISH_FIELD_LABELS,
  statusBadgeVariant,
  type EventDetail,
  type EventPatchBody,
} from "@/lib/events-api";

import { EventCoverImage } from "./event-cover-image";

import { TicketTypesSection } from "./ticket-types-section";

type EventDetailFormProps = {
  eventId: string;
};

export function EventDetailForm({ eventId }: EventDetailFormProps) {
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [ticketTypeCount, setTicketTypeCount] = useState(0);

  const [status, setStatus] = useState("draft");
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [timezone, setTimezone] = useState("America/New_York");
  const [startsAtLocal, setStartsAtLocal] = useState("");
  const [endsAtLocal, setEndsAtLocal] = useState("");
  const [venueName, setVenueName] = useState("");
  const [venueAddress, setVenueAddress] = useState("");
  const [description, setDescription] = useState("");
  const [coverImageUrl, setCoverImageUrl] = useState<string | null>(null);

  const applyEvent = useCallback((event: EventDetail) => {
    const tz = event.timezone ?? "America/New_York";
    setStatus(event.status);
    setName(event.name);
    setSlug(event.slug);
    setTimezone(tz);
    setStartsAtLocal(isoToDateTimeLocal(event.starts_at, tz));
    setEndsAtLocal(isoToDateTimeLocal(event.ends_at, tz));
    setVenueName(event.venue_name ?? "");
    setVenueAddress(event.venue_address ?? "");
    setDescription(event.description ?? "");
    setCoverImageUrl(event.cover_image_url);
  }, []);

  const loadEvent = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const event = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`);
      applyEvent(event);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "Failed to load event");
    } finally {
      setLoading(false);
    }
  }, [applyEvent, eventId]);

  useEffect(() => {
    void loadEvent();
  }, [loadEvent]);

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);

    try {
      const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`, {
        method: "PATCH",
        body: JSON.stringify({
          name,
          slug,
          starts_at: dateTimeLocalToISO(startsAtLocal, timezone),
          ends_at: dateTimeLocalToISO(endsAtLocal, timezone),
          timezone,
          venue_name: venueName,
          venue_address: venueAddress,
          description,
        }),
      });
      applyEvent(updated);
      toast.success("Event saved");
      router.refresh();
    } catch (saveError) {
      toast.error(saveError instanceof Error ? saveError.message : "Failed to save event");
    } finally {
      setSaving(false);
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

  async function handlePublish() {
    setPublishing(true);
    try {
      const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}/publish`, {
        method: "POST",
      });
      applyEvent(updated);
      toast.success("Event published");
      router.refresh();
    } catch (publishError) {
      toast.error(publishError instanceof Error ? publishError.message : "Failed to publish event");
    } finally {
      setPublishing(false);
    }
  }

  async function handleCancel() {
    setCancelling(true);
    try {
      const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}/cancel`, {
        method: "POST",
      });
      applyEvent(updated);
      setCancelOpen(false);
      toast.success("Event cancelled");
      router.refresh();
    } catch (cancelError) {
      toast.error(cancelError instanceof Error ? cancelError.message : "Failed to cancel event");
    } finally {
      setCancelling(false);
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading event...</p>;
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load event</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  const slugReadOnly = status !== "draft";
  const publishMissingFields = getPublishMissingFields(name, slug, startsAtLocal, timezone, ticketTypeCount);
  const canPublish = status === "draft" && publishMissingFields.length === 0;

  const statusDescription =
    status === "draft"
      ? "Edit scheduling, venue, and description. Publish when requirements are met."
      : status === "published"
        ? "This event is live in your catalog. Cancel it when it is no longer active."
        : "This event was cancelled and remains in your catalog for reference.";

  const patchBody: EventPatchBody = {
    name,
    slug,
    starts_at: dateTimeLocalToISO(startsAtLocal, timezone),
    ends_at: dateTimeLocalToISO(endsAtLocal, timezone),
    timezone,
    venue_name: venueName,
    venue_address: venueAddress,
    description,
  };

  return (
    <div className="space-y-6">
      <nav className="text-sm text-muted-foreground">
        <Link href="/events" className="text-primary hover:underline">
          Events
        </Link>
        <span className="px-2">/</span>
        <span className="text-foreground">{name || "Event"}</span>
      </nav>

      <PageHeader
        title={name || "Event"}
        description={statusDescription}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={statusBadgeVariant(status)} className="capitalize">
              {status}
            </Badge>
            {status === "draft" ? (
              <>
                <Button type="button" disabled={!canPublish || publishing} onClick={() => void handlePublish()}>
                  {publishing ? "Publishing..." : "Publish event"}
                </Button>
                <Button type="button" variant="destructive" onClick={() => setDeleteOpen(true)}>
                  Delete event
                </Button>
              </>
            ) : null}
            {status === "published" ? (
              <Button type="button" variant="destructive" onClick={() => setCancelOpen(true)}>
                Cancel event
              </Button>
            ) : null}
          </div>
        }
      />

      {status === "draft" && publishMissingFields.length > 0 ? (
        <Alert>
          <AlertTitle>Publish requirements</AlertTitle>
          <AlertDescription>
            Before publishing, complete:{" "}
            {publishMissingFields.map((field) => PUBLISH_FIELD_LABELS[field] ?? field).join(", ")}.
          </AlertDescription>
        </Alert>
      ) : null}

      <form className="space-y-6" onSubmit={(event) => void handleSave(event)}>
        <Card>
          <CardHeader>
            <CardTitle>Details</CardTitle>
            <CardDescription>Name, slug, and scheduling for this Event.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <FormField id="detail-name" label="Name">
              <Input value={name} onChange={(event) => setName(event.target.value)} required />
            </FormField>
            <FormField
              id="detail-slug"
              label="Slug"
              description={slugReadOnly ? "Locked after publish." : "Editable while the event is a draft."}
            >
              <Input value={slug} onChange={(event) => setSlug(event.target.value)} readOnly={slugReadOnly} required />
            </FormField>
            <FormField id="detail-timezone" label="Timezone">
              <select
                id="detail-timezone"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={timezone}
                onChange={(event) => setTimezone(event.target.value)}
              >
                {COMMON_TIMEZONES.map((tz) => (
                  <option key={tz} value={tz}>
                    {tz}
                  </option>
                ))}
              </select>
            </FormField>
            <div className="grid gap-4 md:grid-cols-2">
              <FormField id="detail-starts-at" label="Starts at">
                <Input
                  id="detail-starts-at"
                  type="datetime-local"
                  value={startsAtLocal}
                  onChange={(event) => setStartsAtLocal(event.target.value)}
                />
              </FormField>
              <FormField id="detail-ends-at" label="Ends at">
                <Input
                  id="detail-ends-at"
                  type="datetime-local"
                  value={endsAtLocal}
                  onChange={(event) => setEndsAtLocal(event.target.value)}
                />
              </FormField>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Venue</CardTitle>
            <CardDescription>Optional location details for customers.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <FormField id="detail-venue-name" label="Venue name">
              <Input value={venueName} onChange={(event) => setVenueName(event.target.value)} />
            </FormField>
            <FormField id="detail-venue-address" label="Venue address">
              <Input value={venueAddress} onChange={(event) => setVenueAddress(event.target.value)} />
            </FormField>
          </CardContent>
        </Card>

        <EventCoverImage
          eventId={eventId}
          coverImageUrl={coverImageUrl}
          patchBody={patchBody}
          onUpdated={(event) => {
            applyEvent(event);
            router.refresh();
          }}
        />

        <Card>
          <CardHeader>
            <CardTitle>Description</CardTitle>
            <CardDescription>Markdown supported.</CardDescription>
          </CardHeader>
          <CardContent>
            <FormField id="detail-description" label="Description">
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                rows={8}
                placeholder="Tell customers what to expect..."
              />
            </FormField>
          </CardContent>
        </Card>

        <Button type="submit" disabled={saving}>
          {saving ? "Saving..." : "Save changes"}
        </Button>
      </form>

      <TicketTypesSection
        eventId={eventId}
        eventStatus={status}
        onTicketTypeCountChange={setTicketTypeCount}
      />

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
