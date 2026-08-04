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
import { isValidRegistrationURL, type RegistrationMode } from "@/lib/registration";
import {
  dateTimeLocalToISO,
  fetchEventsJSON,
  getTimezoneOptions,
  isoToDateTimeLocal,
  type EventDetail,
  type EventPatchBody,
} from "@/lib/events-api";

import { EventCoverImage } from "./event-cover-image";
import { EventCoverVideo } from "./event-cover-video";
import { EventTagsSection } from "./event-tags-section";

type EventDetailFormProps = {
  eventId: string;
  canManageTags: boolean;
};

export function EventDetailForm({ eventId, canManageTags }: EventDetailFormProps) {
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
  const [coverVideoUrl, setCoverVideoUrl] = useState<string | null>(null);
  const [feeHandling, setFeeHandling] = useState<FeeHandling>("pass_on");
  const [registrationMode, setRegistrationMode] = useState<RegistrationMode>("tickets");
  const [registrationUrl, setRegistrationUrl] = useState("");

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
    setCoverVideoUrl(event.cover_video_url);
    setFeeHandling(event.fee_handling);
    setRegistrationMode(event.registration_mode);
    setRegistrationUrl(event.registration_url ?? "");
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

  // Mirrors the backend's https-only allowlist so a bad Registration Link is
  // caught as it is typed. The backend's check is the authoritative one — this
  // is feedback, not the control (ADR 0028). An empty link is not an error: the
  // mode may be chosen before the registration page exists.
  const registrationUrlError =
    registrationMode === "external" &&
    registrationUrl.trim() !== "" &&
    !isValidRegistrationURL(registrationUrl)
      ? "Must be an https link, like https://lu.ma/your-event."
      : null;

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
          registration_mode: registrationMode,
          registration_url: registrationUrl,
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
    if (registrationUrlError) {
      toast.error(registrationUrlError);
      return;
    }
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
  // The registration mode is settled while the Event is a draft. Once published
  // it is frozen in both directions, and a new Event is the way to change your
  // mind (ADR 0028).
  const modeLocked = status !== "draft";

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
        coverVideoUrl={coverVideoUrl}
        patchBody={patchBody}
        onUpdated={(event) => {
          applyEvent(event);
          router.refresh();
        }}
      />

      <EventCoverVideo
        eventId={eventId}
        coverVideoUrl={coverVideoUrl}
        coverImageUrl={coverImageUrl}
        patchBody={patchBody}
        onUpdated={(event) => {
          applyEvent(event);
          router.refresh();
        }}
      />

      <Card>
        <CardHeader>
          <CardTitle>Registration</CardTitle>
          <CardDescription>
            Sell tickets here, or send people to another site to sign up. An event does one or the
            other, and the choice is settled while it is a draft.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={registrationMode === "tickets" ? "secondary" : "outline"}
              aria-pressed={registrationMode === "tickets"}
              disabled={modeLocked}
              onClick={() => setRegistrationMode("tickets")}
            >
              Sell tickets here
            </Button>
            <Button
              type="button"
              variant={registrationMode === "external" ? "secondary" : "outline"}
              aria-pressed={registrationMode === "external"}
              disabled={modeLocked}
              onClick={() => setRegistrationMode("external")}
            >
              Register on another site
            </Button>
          </div>
          {registrationMode === "external" ? (
            <FormField
              id="detail-registration-url"
              label="Registration link"
              description="The https page people sign up on — a Luma page, an Eventbrite listing, a form. You can choose this mode now and add the link later."
              error={registrationUrlError}
            >
              <Input
                id="detail-registration-url"
                type="url"
                inputMode="url"
                value={registrationUrl}
                onChange={(event) => setRegistrationUrl(event.target.value)}
                placeholder="https://lu.ma/your-event"
              />
            </FormField>
          ) : (
            <p className="text-sm text-muted-foreground">
              People buy ticket types on this event&apos;s page.
            </p>
          )}
          {modeLocked ? (
            <p className="text-sm text-muted-foreground">
              Locked after publish. Create a new event to change how this one takes sign-ups.
            </p>
          ) : null}
        </CardContent>
      </Card>

      {/* Fee Handling is hidden on an externally registered Event: no Ticket Sale
          is ever made here, so no Platform Fee is ever charged, and asking who
          absorbs a fee that will never exist is a confusing question. */}
      {registrationMode === "external" ? null : (
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
      )}

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

      {/* Tags carry their own whole-set save, like the cover image and video
          above, so they stay out of this form's submit. */}
      {canManageTags ? <EventTagsSection eventId={eventId} /> : null}

      <Button type="submit" disabled={saving}>
        {saving ? "Saving..." : "Save changes"}
      </Button>
    </form>
  );
}
