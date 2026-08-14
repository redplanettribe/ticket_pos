"use client";

import { useMessages, useTranslations } from "next-intl";
import { ChangeEvent, useRef, useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import {
  ApiError,
  fetchEventsJSON,
  type CoverUploadURL,
  type EventDetail,
  type EventPatchBody,
} from "@/lib/events-api";
import {
  MAX_COVER_VIDEO_BYTES,
  MAX_COVER_VIDEO_SECONDS,
  MIN_COVER_VIDEO_WIDTH,
  validateCoverVideo,
  type CoverVideoFile,
  type CoverVideoRejectionReason,
} from "@/lib/cover-video";

/**
 * The rule's own numbers, handed to the catalog as ICU arguments so the
 * sentence about a limit and the limit itself cannot drift apart. Strings, not
 * numbers: 1280 is a pixel count and reads as "1280" in both languages, where
 * an ICU number argument would group it into "1,280" / "1.280".
 */
const COVER_VIDEO_LIMITS = {
  width: String(MIN_COVER_VIDEO_WIDTH),
  seconds: String(MAX_COVER_VIDEO_SECONDS),
  megabytes: String(MAX_COVER_VIDEO_BYTES / 1024 / 1024),
};

/** The `event` catalog key a rejection reason is said with. */
function rejectionKey(reason: CoverVideoRejectionReason) {
  return `coverVideoReject${reason[0].toUpperCase()}${reason.slice(1)}` as
    | "coverVideoRejectType"
    | "coverVideoRejectUnreadable"
    | "coverVideoRejectSize"
    | "coverVideoRejectWidth"
    | "coverVideoRejectAspect"
    | "coverVideoRejectDuration";
}

/**
 * The chosen file's dimensions and duration, read the only way a browser
 * offers them: load its metadata into an off-DOM video element. Resolves with
 * zeroed measurements when the file has no readable video track, which the
 * validation rejects on its own terms.
 */
function readVideoMetadata(file: File): Promise<Pick<CoverVideoFile, "width" | "height" | "duration">> {
  return new Promise((resolve) => {
    const objectUrl = URL.createObjectURL(file);
    const probe = document.createElement("video");
    probe.preload = "metadata";

    const finish = (measurements: Pick<CoverVideoFile, "width" | "height" | "duration">) => {
      URL.revokeObjectURL(objectUrl);
      probe.removeAttribute("src");
      resolve(measurements);
    };

    probe.onloadedmetadata = () =>
      finish({ width: probe.videoWidth, height: probe.videoHeight, duration: probe.duration });
    probe.onerror = () => finish({ width: 0, height: 0, duration: 0 });
    probe.src = objectUrl;
  });
}

type EventCoverVideoProps = {
  eventId: string;
  coverVideoUrl: string | null;
  coverImageUrl: string | null;
  patchBody: EventPatchBody;
  onUpdated: (event: EventDetail) => void;
};

export function EventCoverVideo({
  eventId,
  coverVideoUrl,
  coverImageUrl,
  patchBody,
  onUpdated,
}: EventCoverVideoProps) {
  const t = useTranslations("event");
  const errorCopy = useMessages().errors;
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);

  async function attachVideo(objectKey: string) {
    const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`, {
      method: "PATCH",
      body: JSON.stringify({
        ...patchBody,
        cover_video_key: objectKey,
      }),
    });
    onUpdated(updated);
  }

  async function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    // Every constraint is checked here, before a single byte is uploaded:
    // ADR 0020 stores the file verbatim, so the browser is the only enforcer.
    const measurements = file.type === "video/mp4"
      ? await readVideoMetadata(file)
      : { width: 0, height: 0, duration: 0 };
    const validation = validateCoverVideo({ ...measurements, size: file.size, type: file.type });
    if (!validation.ok) {
      // The module names the requirement; the catalog says what it means.
      toast.error(t(rejectionKey(validation.reason), COVER_VIDEO_LIMITS));
      return;
    }

    setUploading(true);
    try {
      const presign = await fetchEventsJSON<CoverUploadURL>(`/api/events/${eventId}/video-upload-url`, {
        method: "POST",
        body: JSON.stringify({
          content_type: file.type,
          file_name: file.name,
        }),
      });

      const uploadResponse = await fetch(presign.upload_url, {
        method: "PUT",
        headers: { "Content-Type": file.type },
        body: file,
      });
      if (!uploadResponse.ok) {
        // The object store, not the API: no error envelope, so no code to key
        // a sentence on — the surface's own is the whole answer.
        throw new Error("cover video upload rejected by object storage");
      }

      await attachVideo(presign.object_key);
      toast.success(t("coverVideoUploadedToast"));
    } catch (uploadError) {
      toast.error(
        apiErrorMessage(errorCopy, uploadError instanceof ApiError ? uploadError : null) ??
          t("coverVideoUploadFailed"),
      );
    } finally {
      setUploading(false);
    }
  }

  async function handleRemove() {
    setRemoving(true);
    try {
      const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`, {
        method: "PATCH",
        body: JSON.stringify({
          ...patchBody,
          cover_video_key: "",
        }),
      });
      onUpdated(updated);
      toast.success(t("coverVideoRemovedToast"));
    } catch (removeError) {
      toast.error(
        apiErrorMessage(errorCopy, removeError instanceof ApiError ? removeError : null) ??
          t("coverVideoRemoveFailed"),
      );
    } finally {
      setRemoving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("coverVideoTitle")}</CardTitle>
        <CardDescription>{t("coverVideoDescription", COVER_VIDEO_LIMITS)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {coverVideoUrl ? (
          <div className="overflow-hidden rounded-md border bg-muted/30">
            <video
              src={coverVideoUrl}
              poster={coverImageUrl ?? undefined}
              muted
              loop
              playsInline
              autoPlay
              preload="metadata"
              className="max-h-64 w-full object-cover"
            />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t("coverVideoNone")}</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="video/mp4"
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        {coverImageUrl ? null : (
          <p className="text-sm text-muted-foreground">{t("coverVideoPosterHint")}</p>
        )}

        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={uploading || removing || !coverImageUrl}
            onClick={() => inputRef.current?.click()}
          >
            {uploading
              ? t("uploading")
              : coverVideoUrl
                ? t("coverVideoReplace")
                : t("coverVideoUpload")}
          </Button>
          {coverVideoUrl ? (
            <Button type="button" variant="ghost" disabled={uploading || removing} onClick={() => void handleRemove()}>
              {removing ? t("removing") : t("coverVideoRemove")}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
