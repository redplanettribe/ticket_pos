"use client";

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

import {
  fetchEventsJSON,
  type CoverUploadURL,
  type EventDetail,
  type EventPatchBody,
} from "@/lib/events-api";
import { validateCoverVideo, type CoverVideoFile } from "@/lib/cover-video";

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
      toast.error(validation.message);
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
        throw new Error("Failed to upload cover video.");
      }

      await attachVideo(presign.object_key);
      toast.success("Cover video uploaded");
    } catch (uploadError) {
      toast.error(uploadError instanceof Error ? uploadError.message : "Failed to upload cover video");
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
      toast.success("Cover video removed");
    } catch (removeError) {
      toast.error(removeError instanceof Error ? removeError.message : "Failed to remove cover video");
    } finally {
      setRemoving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Cover video</CardTitle>
        <CardDescription>
          MP4, landscape 16:9, at least 1280 pixels wide, up to 30 seconds and 50 MB. Plays muted and looping in
          the event page hero, over the cover image.
        </CardDescription>
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
          <p className="text-sm text-muted-foreground">No cover video yet.</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="video/mp4"
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        {coverImageUrl ? null : (
          <p className="text-sm text-muted-foreground">Add a cover image first — it&apos;s the video&apos;s poster.</p>
        )}

        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={uploading || removing || !coverImageUrl}
            onClick={() => inputRef.current?.click()}
          >
            {uploading ? "Uploading..." : coverVideoUrl ? "Replace video" : "Upload video"}
          </Button>
          {coverVideoUrl ? (
            <Button type="button" variant="ghost" disabled={uploading || removing} onClick={() => void handleRemove()}>
              {removing ? "Removing..." : "Remove video"}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
