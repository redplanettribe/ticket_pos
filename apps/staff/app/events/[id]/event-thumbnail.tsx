"use client";

import { useMessages, useTranslations } from "next-intl";
import { ChangeEvent, useEffect, useRef, useState } from "react";

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
  THUMBNAIL_EXPORT_QUALITY,
  THUMBNAIL_EXPORT_TYPE,
  THUMBNAIL_FILE_TYPES,
  THUMBNAIL_LIMITS,
  thumbnailExportSize,
  thumbnailRejectionKey,
  validateThumbnailCrop,
  validateThumbnailFile,
  validateThumbnailSource,
  type ThumbnailCrop,
} from "@/lib/thumbnail-crop";

import { ThumbnailCropDialog, type ThumbnailSource } from "./thumbnail-crop-dialog";

/**
 * The image bytes decoded into an image, or null when the browser cannot read
 * them. The object URL lives until the crop dialog closes.
 */
function decodeImage(bytes: Blob): Promise<ThumbnailSource | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(bytes);
    const image = new Image();
    image.onload = () => resolve({ image, url, width: image.naturalWidth, height: image.naturalHeight });
    image.onerror = () => {
      URL.revokeObjectURL(url);
      resolve(null);
    };
    image.src = url;
  });
}

/**
 * The current Cover Image, read from the public bucket, or null when it cannot
 * be fetched. Read as bytes through a CORS request rather than pointed at by an
 * image element: the canvas then draws a same-origin object URL and is never
 * tainted, exactly like an uploaded file. The bucket allows the Staff origin to
 * GET (terraform/modules/ticket-pos/storage.tf; MinIO allows any origin).
 * no-store skips a cached copy fetched without CORS by the cover's own preview,
 * which carries no Access-Control-Allow-Origin and would be refused.
 */
async function fetchCoverImage(coverImageUrl: string): Promise<Blob | null> {
  try {
    const response = await fetch(coverImageUrl, { mode: "cors", credentials: "omit", cache: "no-store" });
    return response.ok ? await response.blob() : null;
  } catch {
    return null;
  }
}

/**
 * The crop drawn onto a white canvas, so a transparent PNG comes out on white
 * rather than black, and encoded as the one type the upload endpoint takes.
 */
function exportThumbnail(source: ThumbnailSource, crop: ThumbnailCrop): Promise<Blob> {
  const size = thumbnailExportSize(crop);
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (!context) {
    return Promise.reject(new Error("canvas 2d context unavailable"));
  }
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, size, size);
  context.imageSmoothingQuality = "high";
  context.drawImage(source.image, crop.x, crop.y, crop.width, crop.height, 0, 0, size, size);
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("canvas could not encode the thumbnail"))),
      THUMBNAIL_EXPORT_TYPE,
      THUMBNAIL_EXPORT_QUALITY,
    );
  });
}

type EventThumbnailProps = {
  eventId: string;
  thumbnailUrl: string | null;
  // Never written. Previewed: with no Thumbnail, the Discover Timeline shows
  // the Cover Image cropped to its centre, and this card says so. And offered
  // as a source: "Crop from cover" frames a square over it.
  coverImageUrl: string | null;
  patchBody: EventPatchBody;
  onUpdated: (event: EventDetail) => void;
};

export function EventThumbnail({ eventId, thumbnailUrl, coverImageUrl, patchBody, onUpdated }: EventThumbnailProps) {
  const t = useTranslations("event");
  const errorCopy = useMessages().errors;
  const inputRef = useRef<HTMLInputElement>(null);
  const [source, setSource] = useState<ThumbnailSource | null>(null);
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [loadingCover, setLoadingCover] = useState(false);

  useEffect(() => {
    if (!source) {
      return;
    }
    return () => URL.revokeObjectURL(source.url);
  }, [source]);

  async function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    // Every rule is checked before a byte is uploaded: the server stores what
    // it is given (ADR 0020), so the browser is the only enforcer.
    const fileValidation = validateThumbnailFile(file);
    if (!fileValidation.ok) {
      toast.error(t(thumbnailRejectionKey(fileValidation.reason), THUMBNAIL_LIMITS));
      return;
    }

    const decoded = await decodeImage(file);
    if (!decoded) {
      toast.error(t("thumbnailUnreadable"));
      return;
    }
    openCropDialog(decoded);
  }

  /**
   * Opens the crop dialog on a decoded source, whether an uploaded file or the
   * Cover Image. A source too small for any acceptable square is refused now,
   * with the one sentence both paths share, rather than opening a dialog whose
   * every position would be refused.
   */
  function openCropDialog(decoded: ThumbnailSource) {
    const sourceValidation = validateThumbnailSource(decoded);
    if (!sourceValidation.ok) {
      URL.revokeObjectURL(decoded.url);
      toast.error(t(thumbnailRejectionKey(sourceValidation.reason), THUMBNAIL_LIMITS));
      return;
    }
    setSource(decoded);
  }

  // The Cover Image passed the Cover Image's own file rules when it was
  // uploaded, which are the Thumbnail's too, so only the source size is checked.
  // The Thumbnail is cropped from a copy of its bytes: it keeps no link to the
  // Cover Image, and replacing or removing the cover later leaves it alone.
  async function handleCropFromCover() {
    if (!coverImageUrl) {
      return;
    }
    setLoadingCover(true);
    try {
      const bytes = await fetchCoverImage(coverImageUrl);
      const decoded = bytes ? await decodeImage(bytes) : null;
      if (!decoded) {
        toast.error(t("thumbnailCoverUnavailable"));
        return;
      }
      openCropDialog(decoded);
    } finally {
      setLoadingCover(false);
    }
  }

  async function handleConfirm(crop: ThumbnailCrop) {
    if (!source) {
      return;
    }
    const validation = validateThumbnailCrop(source, crop);
    if (!validation.ok) {
      toast.error(t(thumbnailRejectionKey(validation.reason), THUMBNAIL_LIMITS));
      return;
    }

    setUploading(true);
    try {
      const blob = await exportThumbnail(source, crop);
      const presign = await fetchEventsJSON<CoverUploadURL>(`/api/events/${eventId}/thumbnail-upload-url`, {
        method: "POST",
        body: JSON.stringify({ content_type: THUMBNAIL_EXPORT_TYPE }),
      });

      const uploadResponse = await fetch(presign.upload_url, {
        method: "PUT",
        headers: { "Content-Type": THUMBNAIL_EXPORT_TYPE },
        body: blob,
      });
      if (!uploadResponse.ok) {
        // The object store, not the API: no error envelope to read a code off.
        throw new Error("thumbnail upload rejected by object storage");
      }

      const updated = await fetchEventsJSON<EventDetail>(`/api/events/${eventId}`, {
        method: "PATCH",
        body: JSON.stringify({ ...patchBody, thumbnail_key: presign.object_key }),
      });
      onUpdated(updated);
      setSource(null);
      toast.success(t("thumbnailUploadedToast"));
    } catch (uploadError) {
      // The dialog stays open on the same framing, so trying again is one click.
      toast.error(
        apiErrorMessage(errorCopy, uploadError instanceof ApiError ? uploadError : null) ??
          t("thumbnailUploadFailed"),
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
        body: JSON.stringify({ ...patchBody, thumbnail_key: "" }),
      });
      onUpdated(updated);
      toast.success(t("thumbnailRemovedToast"));
    } catch (removeError) {
      toast.error(
        apiErrorMessage(errorCopy, removeError instanceof ApiError ? removeError : null) ??
          t("thumbnailRemoveFailed"),
      );
    } finally {
      setRemoving(false);
    }
  }

  const previewUrl = thumbnailUrl ?? coverImageUrl;
  const busy = uploading || removing || loadingCover;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("thumbnailTitle")}</CardTitle>
        <CardDescription>{t("thumbnailDescription", THUMBNAIL_LIMITS)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {previewUrl ? (
          <div className="flex items-center gap-4">
            {/* The Timeline row card's own frame: 96 px, 112 px from the sm breakpoint. */}
            <div className="relative h-24 w-24 shrink-0 overflow-hidden rounded-md bg-muted sm:h-28 sm:w-28">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={previewUrl}
                alt={thumbnailUrl ? t("thumbnailAlt") : t("thumbnailFallbackAlt")}
                className="h-full w-full object-cover"
              />
            </div>
            <p className="text-sm text-muted-foreground">
              {thumbnailUrl ? t("thumbnailPreviewHint") : t("thumbnailFallbackHint")}
            </p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t("thumbnailNone")}</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept={THUMBNAIL_FILE_TYPES.join(",")}
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={busy} onClick={() => inputRef.current?.click()}>
            {uploading ? t("uploading") : thumbnailUrl ? t("thumbnailReplace") : t("thumbnailUpload")}
          </Button>
          {coverImageUrl ? (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void handleCropFromCover()}>
              {loadingCover ? t("thumbnailCoverLoading") : t("thumbnailCropFromCover")}
            </Button>
          ) : null}
          {thumbnailUrl ? (
            <Button type="button" variant="ghost" disabled={busy} onClick={() => void handleRemove()}>
              {removing ? t("removing") : t("thumbnailRemove")}
            </Button>
          ) : null}
        </div>
      </CardContent>

      <ThumbnailCropDialog
        source={source}
        busy={uploading}
        onCancel={() => setSource(null)}
        onConfirm={(crop) => void handleConfirm(crop)}
      />
    </Card>
  );
}
