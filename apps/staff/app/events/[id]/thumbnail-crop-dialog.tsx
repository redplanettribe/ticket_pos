"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useTranslations } from "next-intl";
import { PointerEvent, useEffect, useRef, useState } from "react";

import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@ticket-pos/ui";

import { formatNumber } from "@/lib/format";
import {
  MIN_THUMBNAIL_CROP_PIXELS,
  THUMBNAIL_LIMITS,
  initialThumbnailCrop,
  moveThumbnailCrop,
  thumbnailRejectionKey,
  validateThumbnailCrop,
  zoomThumbnailCrop,
  type ThumbnailCrop,
} from "@/lib/thumbnail-crop";

/** A decoded image ready to frame: its object URL and natural size. */
export type ThumbnailSource = {
  image: HTMLImageElement;
  url: string;
  width: number;
  height: number;
};

type ThumbnailCropDialogProps = {
  source: ThumbnailSource | null;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (crop: ThumbnailCrop) => void;
};

/**
 * A square frame dragged and zoomed over the chosen image. Everything here is
 * screen-pixel plumbing: positions are converted to source pixels and handed
 * to the crop module, which owns where the frame may go and whether it is
 * acceptable.
 */
export function ThumbnailCropDialog({ source, busy, onCancel, onConfirm }: ThumbnailCropDialogProps) {
  const t = useTranslations("event");
  const locale = toAppLocale(useLocale());
  const imageRef = useRef<HTMLImageElement>(null);
  const dragRef = useRef<{ pointerId: number; clientX: number; clientY: number; crop: ThumbnailCrop } | null>(null);
  const [crop, setCrop] = useState<ThumbnailCrop | null>(null);

  useEffect(() => {
    setCrop(source ? initialThumbnailCrop(source) : null);
  }, [source]);

  if (!source || !crop) {
    return null;
  }

  const maxSide = Math.min(source.width, source.height);
  // Zooming in shrinks the square; the slider never goes under the minimum, so
  // only a source that cannot hold an acceptable square is ever refused here.
  const minSide = Math.min(MIN_THUMBNAIL_CROP_PIXELS, maxSide);
  const validation = validateThumbnailCrop(source, crop);
  // The slider reads as zoom, right is closer: its value runs opposite to the
  // square's side, so the one mapping serves both directions.
  const sliderForSide = (side: number) => maxSide + minSide - side;

  /** Source pixels per screen pixel, from the image as it is laid out now. */
  function sourceScale() {
    const rendered = imageRef.current?.getBoundingClientRect().width ?? 0;
    return rendered > 0 ? source!.width / rendered : 1;
  }

  function handlePointerDown(event: PointerEvent<HTMLDivElement>) {
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { pointerId: event.pointerId, clientX: event.clientX, clientY: event.clientY, crop: crop! };
  }

  function handlePointerMove(event: PointerEvent<HTMLDivElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) {
      return;
    }
    const scale = sourceScale();
    setCrop(
      moveThumbnailCrop(
        source!,
        drag.crop,
        (event.clientX - drag.clientX) * scale,
        (event.clientY - drag.clientY) * scale,
      ),
    );
  }

  function handlePointerUp(event: PointerEvent<HTMLDivElement>) {
    if (dragRef.current?.pointerId === event.pointerId) {
      dragRef.current = null;
    }
  }

  const percent = (value: number, total: number) => `${(value / total) * 100}%`;

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("thumbnailCropTitle")}</DialogTitle>
          <DialogDescription>{t("thumbnailCropDescription")}</DialogDescription>
        </DialogHeader>

        <div className="flex justify-center">
          {/* overflow-hidden keeps the square's dimming shadow on the image:
              unclipped, its 9999px spread darkens the whole dialog. */}
          <div className="relative inline-block touch-none select-none overflow-hidden">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              ref={imageRef}
              src={source.url}
              alt={t("thumbnailCropImageAlt")}
              draggable={false}
              className="block max-h-[60vh] max-w-full"
            />
            <div
              className="absolute cursor-move border-2 border-white shadow-[0_0_0_9999px_rgba(0,0,0,0.55)] outline outline-1 outline-black/40"
              style={{
                left: percent(crop.x, source.width),
                top: percent(crop.y, source.height),
                width: percent(crop.width, source.width),
                height: percent(crop.height, source.height),
              }}
              onPointerDown={handlePointerDown}
              onPointerMove={handlePointerMove}
              onPointerUp={handlePointerUp}
              onPointerCancel={handlePointerUp}
            />
          </div>
        </div>

        <div className="space-y-2">
          <label className="flex items-center gap-3 text-sm">
            <span className="shrink-0">{t("thumbnailCropZoom")}</span>
            <input
              type="range"
              // The dialog focuses the slider on open, so it wears the design
              // system's ring and colour rather than the browser's defaults.
              className="w-full cursor-pointer rounded-full accent-primary ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
              min={minSide}
              max={maxSide}
              step={1}
              // Right is zoomed in: a smaller square over the source.
              value={sliderForSide(crop.width)}
              disabled={busy || minSide === maxSide}
              onChange={(event) =>
                setCrop(zoomThumbnailCrop(source, crop, sliderForSide(Number(event.target.value))))
              }
            />
          </label>
          <p className="text-xs text-muted-foreground">{t("thumbnailCropSize", { size: formatNumber(crop.width, locale) })}</p>
          {validation.ok ? null : (
            <p role="alert" className="text-sm text-destructive">
              {t(thumbnailRejectionKey(validation.reason), THUMBNAIL_LIMITS)}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="ghost" disabled={busy} onClick={onCancel}>
            {t("thumbnailCropCancel")}
          </Button>
          <Button type="button" disabled={busy || !validation.ok} onClick={() => onConfirm(crop)}>
            {busy ? t("uploading") : t("thumbnailCropConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
