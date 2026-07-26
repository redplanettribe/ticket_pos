"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Combobox,
  FormField,
  Input,
  Textarea,
  toast,
} from "@ticket-pos/ui";

import type { FeeHandling } from "@/lib/fees";
import {
  dateTimeLocalToISO,
  fetchEventsJSON,
  getTimezoneOptions,
  isoToDateTimeLocal,
  type EventDetail,
  type EventPatchBody,
} from "@/lib/events-api";

import { EventCoverImage } from "./event-cover-image";

type EventDetailFormProps = {
  eventId: string;
};

export function EventDetailForm({ eventId }: EventDetailFormProps) {
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
  const [feeHandling, setFeeHandling] = useState<FeeHandling>("pass_on");

  const timezoneOptions = useMemo(() => getTimezoneOptions(), []);

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
    setFeeHandling(event.fee_handling);
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

  async function saveEvent(): Promise<EventDetail | null> {
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
          fee_handling: feeHandling,
        }),
      });
      applyEvent(updated);
      return updated;
    } catch (saveError) {
      toast.error(saveError instanceof Error ? saveError.message : "Failed to save event");
      return null;
    }
  }

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    try {
      const updated = await saveEvent();
      if (updated) {
        toast.success("Event saved");
        // Refresh so the persistent header bar re-reads saved server state and
        // its Publish button reflects the newly-saved Event (Save-then-Publish).
        router.refresh();
      }
    } finally {
      setSaving(false);
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

  const patchBody: EventPatchBody = {
    name,
    slug,
    starts_at: dateTimeLocalToISO(startsAtLocal, timezone),
    ends_at: dateTimeLocalToISO(endsAtLocal, timezone),
    timezone,
    venue_name: venueName,
    venue_address: venueAddress,
    description,
    fee_handling: feeHandling,
  };

  return (
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
            <Input
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              readOnly={slugReadOnly}
              required
            />
          </FormField>
          <FormField id="detail-timezone" label="Timezone">
            <Combobox
              options={timezoneOptions}
              value={timezone}
              onValueChange={setTimezone}
              placeholder="Select a timezone"
              searchPlaceholder="Search timezones…"
              emptyText="No matching timezone."
            />
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
          <CardTitle>Service fee</CardTitle>
          <CardDescription>
            Choose whether buyers cover the platform&apos;s service fee or you absorb it out of your
            prices. Changes apply to future sales only.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={feeHandling === "pass_on" ? "secondary" : "outline"}
              aria-pressed={feeHandling === "pass_on"}
              onClick={() => setFeeHandling("pass_on")}
            >
              Buyers cover it
            </Button>
            <Button
              type="button"
              variant={feeHandling === "absorb" ? "secondary" : "outline"}
              aria-pressed={feeHandling === "absorb"}
              onClick={() => setFeeHandling("absorb")}
            >
              I absorb it
            </Button>
          </div>
          <p className="text-sm text-muted-foreground">
            {feeHandling === "pass_on"
              ? "Buyers pay a little above your ticket prices, and you receive exactly the price you set."
              : "Buyers pay exactly the price you set, and the service fee comes out of it."}
          </p>
        </CardContent>
      </Card>

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
  );
}
