"use client";

import { useMessages, useTranslations } from "next-intl";
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

import { apiErrorMessage } from "@/lib/api-errors";
import type { FeeHandling } from "@/lib/fees";
import { isValidRegistrationURL, type RegistrationMode } from "@/lib/registration";
import {
  ApiError,
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
  const t = useTranslations("event");
  const errorCopy = useMessages().errors;
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
      setError(
        apiErrorMessage(errorCopy, loadError instanceof ApiError ? loadError : null) ??
          t("loadFailed"),
      );
    } finally {
      setLoading(false);
    }
  }, [applyEvent, errorCopy, eventId, t]);

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
      ? t("registrationUrlInvalid")
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
          // Only sent while the field is on screen. Sending it in `tickets` mode
          // would submit a value the organizer cannot see and the form does not
          // validate, so a link typed and then abandoned would fail the save
          // with nothing to point at. Omitting it leaves the stored link alone,
          // which is what a mode flip should do anyway (ADR 0028).
          ...(registrationMode === "external" ? { registration_url: registrationUrl } : {}),
        }),
      });
      applyEvent(updated);
      return updated;
    } catch (saveError) {
      toast.error(
        apiErrorMessage(errorCopy, saveError instanceof ApiError ? saveError : null) ??
          t("saveFailed"),
      );
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
        toast.success(t("savedToast"));
        // Refresh so the persistent header bar re-reads saved server state and
        // its Publish button reflects the newly-saved Event (Save-then-Publish).
        router.refresh();
      }
    } finally {
      setSaving(false);
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
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
          <CardTitle>{t("detailsTitle")}</CardTitle>
          <CardDescription>{t("detailsDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField id="detail-name" label={t("nameLabel")}>
            <Input value={name} onChange={(event) => setName(event.target.value)} required />
          </FormField>
          <FormField
            id="detail-slug"
            label={t("slugLabel")}
            description={slugReadOnly ? t("slugLocked") : t("slugEditable")}
          >
            <Input
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              readOnly={slugReadOnly}
              required
            />
          </FormField>
          <FormField id="detail-timezone" label={t("timezoneLabel")}>
            <Combobox
              options={timezoneOptions}
              value={timezone}
              onValueChange={setTimezone}
              placeholder={t("timezonePlaceholder")}
              searchPlaceholder={t("timezoneSearchPlaceholder")}
              emptyText={t("timezoneEmpty")}
            />
          </FormField>
          <div className="grid gap-4 md:grid-cols-2">
            <FormField id="detail-starts-at" label={t("startsAtLabel")}>
              <Input
                id="detail-starts-at"
                type="datetime-local"
                value={startsAtLocal}
                onChange={(event) => setStartsAtLocal(event.target.value)}
              />
            </FormField>
            <FormField id="detail-ends-at" label={t("endsAtLabel")}>
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
          <CardTitle>{t("venueTitle")}</CardTitle>
          <CardDescription>{t("venueDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField id="detail-venue-name" label={t("venueNameLabel")}>
            <Input value={venueName} onChange={(event) => setVenueName(event.target.value)} />
          </FormField>
          <FormField id="detail-venue-address" label={t("venueAddressLabel")}>
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
          <CardTitle>{t("registrationTitle")}</CardTitle>
          <CardDescription>{t("registrationDescription")}</CardDescription>
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
              {t("registrationModeTickets")}
            </Button>
            <Button
              type="button"
              variant={registrationMode === "external" ? "secondary" : "outline"}
              aria-pressed={registrationMode === "external"}
              disabled={modeLocked}
              onClick={() => setRegistrationMode("external")}
            >
              {t("registrationModeExternal")}
            </Button>
          </div>
          {registrationMode === "external" ? (
            <FormField
              id="detail-registration-url"
              label={t("registrationUrlLabel")}
              description={t("registrationUrlDescription")}
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
            <p className="text-sm text-muted-foreground">{t("registrationTicketsHint")}</p>
          )}
          {modeLocked ? (
            <p className="text-sm text-muted-foreground">{t("registrationLocked")}</p>
          ) : null}
        </CardContent>
      </Card>

      {/* Fee Handling is hidden on an externally registered Event: no Ticket Sale
          is ever made here, so no Platform Fee is ever charged, and asking who
          absorbs a fee that will never exist is a confusing question. */}
      {registrationMode === "external" ? null : (
        <Card>
          <CardHeader>
            <CardTitle>{t("feeTitle")}</CardTitle>
            <CardDescription>{t("feeDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant={feeHandling === "pass_on" ? "secondary" : "outline"}
                aria-pressed={feeHandling === "pass_on"}
                onClick={() => setFeeHandling("pass_on")}
              >
                {t("feePassOn")}
              </Button>
              <Button
                type="button"
                variant={feeHandling === "absorb" ? "secondary" : "outline"}
                aria-pressed={feeHandling === "absorb"}
                onClick={() => setFeeHandling("absorb")}
              >
                {t("feeAbsorb")}
              </Button>
            </div>
            <p className="text-sm text-muted-foreground">
              {feeHandling === "pass_on" ? t("feePassOnHint") : t("feeAbsorbHint")}
            </p>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>{t("descriptionTitle")}</CardTitle>
          <CardDescription>{t("descriptionHint")}</CardDescription>
        </CardHeader>
        <CardContent>
          <FormField id="detail-description" label={t("descriptionLabel")}>
            <Textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              rows={8}
              placeholder={t("descriptionPlaceholder")}
            />
          </FormField>
        </CardContent>
      </Card>

      {/* Tags carry their own whole-set save, like the cover image and video
          above, so they stay out of this form's submit. */}
      {canManageTags ? <EventTagsSection eventId={eventId} /> : null}

      <Button type="submit" disabled={saving}>
        {saving ? t("saving") : t("save")}
      </Button>
    </form>
  );
}
