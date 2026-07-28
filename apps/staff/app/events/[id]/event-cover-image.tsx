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

const MAX_COVER_BYTES = 5 * 1024 * 1024;
const ACCEPTED_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);

type EventCoverImageProps = {
  eventId: string;
  coverImageUrl: string | null;
  // The Cover Image is the Cover Video's poster, so it cannot be removed while a
  // video exists — the server rejects it, and the remove action says so first.
  coverVideoUrl: string | null;
  patchBody: EventPatchBody;
  onUpdated: (event: EventDetail) => void;
};

export function EventCoverImage({
  eventId,
  coverImageUrl,
  coverVideoUrl,
  patchBody,
  onUpdated,
}: EventCoverImageProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);

  async function attachCover(objectKey: string) {
    const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`, {
      method: "PATCH",
      body: JSON.stringify({
        ...patchBody,
        cover_image_key: objectKey,
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

    if (!ACCEPTED_TYPES.has(file.type)) {
      toast.error("Cover image must be JPEG, PNG, or WebP.");
      return;
    }
    if (file.size > MAX_COVER_BYTES) {
      toast.error("Cover image must be 5 MB or smaller.");
      return;
    }

    setUploading(true);
    try {
      const presign = await fetchEventsJSON<CoverUploadURL>(`/api/events/${eventId}/cover-upload-url`, {
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
        throw new Error("Failed to upload cover image.");
      }

      await attachCover(presign.object_key);
      toast.success("Cover image uploaded");
    } catch (uploadError) {
      toast.error(uploadError instanceof Error ? uploadError.message : "Failed to upload cover image");
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
          cover_image_key: "",
        }),
      });
      onUpdated(updated);
      toast.success("Cover image removed");
    } catch (removeError) {
      toast.error(removeError instanceof Error ? removeError.message : "Failed to remove cover image");
    } finally {
      setRemoving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Cover image</CardTitle>
        <CardDescription>JPEG, PNG, or WebP up to 5 MB. Shown on Staff and later on the Storefront.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {coverImageUrl ? (
          <div className="overflow-hidden rounded-md border bg-muted/30">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={coverImageUrl} alt="Event cover preview" className="max-h-64 w-full object-cover" />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No cover image yet.</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        {coverImageUrl && coverVideoUrl ? (
          <p className="text-sm text-muted-foreground">Remove the video first — this image is its poster.</p>
        ) : null}

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={uploading || removing} onClick={() => inputRef.current?.click()}>
            {uploading ? "Uploading..." : coverImageUrl ? "Replace image" : "Upload image"}
          </Button>
          {coverImageUrl ? (
            <Button
              type="button"
              variant="ghost"
              disabled={uploading || removing || Boolean(coverVideoUrl)}
              onClick={() => void handleRemove()}
            >
              {removing ? "Removing..." : "Remove image"}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
