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

// The 50 MB ceiling is the ADR 0020 cap. Aspect, minimum width and duration are
// checked from the file's video metadata in a follow-up; this section enforces
// only what the Cover Image section already does — type and size.
const MAX_VIDEO_BYTES = 50 * 1024 * 1024;
const ACCEPTED_TYPE = "video/mp4";

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

    if (file.type !== ACCEPTED_TYPE) {
      toast.error("Cover video must be an MP4.");
      return;
    }
    if (file.size > MAX_VIDEO_BYTES) {
      toast.error("Cover video must be 50 MB or smaller.");
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
          MP4 up to 50 MB. Plays muted and looping in the event page hero, over the cover image.
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

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={uploading || removing} onClick={() => inputRef.current?.click()}>
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
