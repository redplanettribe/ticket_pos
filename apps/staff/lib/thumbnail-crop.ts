/**
 * Thumbnail crop rules (ADR 0020).
 *
 * The Thumbnail is the browser's own export of a square the Org Admin framed:
 * the server stores the bytes it is given and inspects nothing, so these rules
 * are applied here and only here. Everything is in source pixels, the chosen
 * image's natural size; the crop dialog and the canvas export are thin callers
 * that convert to and from screen pixels around this module.
 *
 * Rejections are tokens. The sentence for each is `event.thumbnailRejectType`
 * and its siblings in the message catalogs, with the numbers below handed in as
 * ICU arguments so a rule and the sentence about it cannot drift apart
 * (ADR 0041).
 */

/** Under this a square looks soft in the Timeline frame on a dense screen. */
export const MIN_THUMBNAIL_CROP_PIXELS = 400;

/** The export's largest side: headroom for square frames larger than today's. */
export const MAX_THUMBNAIL_EXPORT_PIXELS = 1080;

/** The same file limit as the Cover Image. */
export const MAX_THUMBNAIL_FILE_BYTES = 5 * 1024 * 1024;

/** The same file types as the Cover Image. */
export const THUMBNAIL_FILE_TYPES: readonly string[] = ["image/jpeg", "image/png", "image/webp"];

/** The one type the Thumbnail upload-URL endpoint accepts: every canvas can encode it. */
export const THUMBNAIL_EXPORT_TYPE = "image/jpeg";

/** JPEG quality for the export. */
export const THUMBNAIL_EXPORT_QUALITY = 0.9;

/** A source image's natural size. */
export type SourceSize = { width: number; height: number };

/** A rectangle over the source, in source pixels. Valid only when square. */
export type ThumbnailCrop = { x: number; y: number; width: number; height: number };

export type ThumbnailFileRejectionReason = "type" | "size";
export type ThumbnailCropRejectionReason = "small" | "outside" | "square";

export type ThumbnailRejectionReason = ThumbnailFileRejectionReason | ThumbnailCropRejectionReason;

/** The `event` catalog key a rejection is said with. */
export function thumbnailRejectionKey(reason: ThumbnailRejectionReason) {
  return `thumbnailReject${reason[0].toUpperCase()}${reason.slice(1)}` as `thumbnailReject${Capitalize<ThumbnailRejectionReason>}`;
}

/**
 * The rules' own numbers as ICU arguments for the rejection sentences. Strings,
 * not numbers, as for the Cover Video: a pixel limit reads as "400" in both
 * languages, never grouped.
 */
export const THUMBNAIL_LIMITS = {
  pixels: String(MIN_THUMBNAIL_CROP_PIXELS),
  megabytes: String(MAX_THUMBNAIL_FILE_BYTES / 1024 / 1024),
};

export type ThumbnailValidation<Reason> = { ok: true } | { ok: false; reason: Reason };

/** The chosen file's type and size, before it is even decoded. */
export function validateThumbnailFile(file: { type: string; size: number }): ThumbnailValidation<ThumbnailFileRejectionReason> {
  if (!THUMBNAIL_FILE_TYPES.includes(file.type)) {
    return { ok: false, reason: "type" };
  }
  if (file.size > MAX_THUMBNAIL_FILE_BYTES) {
    return { ok: false, reason: "size" };
  }
  return { ok: true };
}

/** A measurement the browser actually resolved. */
function measured(value: number): boolean {
  return Number.isFinite(value) && value > 0;
}

/** The largest square the source holds, centred, on whole pixels. */
export function initialThumbnailCrop(source: SourceSize): ThumbnailCrop {
  const side = measured(source.width) && measured(source.height) ? Math.min(source.width, source.height) : 0;
  return {
    x: Math.floor((source.width - side) / 2) || 0,
    y: Math.floor((source.height - side) / 2) || 0,
    width: side,
    height: side,
  };
}

/**
 * A crop against the rules. Size before position: a source too small for any
 * acceptable square is the thing worth telling the organizer, however the
 * frame sits over it.
 */
export function validateThumbnailCrop(
  source: SourceSize,
  crop: ThumbnailCrop,
): ThumbnailValidation<ThumbnailCropRejectionReason> {
  if (!measured(crop.width) || !measured(crop.height)) {
    return { ok: false, reason: "small" };
  }
  if (crop.width !== crop.height) {
    return { ok: false, reason: "square" };
  }
  if (crop.width < MIN_THUMBNAIL_CROP_PIXELS) {
    return { ok: false, reason: "small" };
  }
  if (crop.x < 0 || crop.y < 0 || crop.x + crop.width > source.width || crop.y + crop.height > source.height) {
    return { ok: false, reason: "outside" };
  }
  return { ok: true };
}

/**
 * Whether a decoded source can yield any acceptable square at all: its largest
 * square checked against the rules. Run before the crop dialog opens, for an
 * uploaded file and for the Cover Image alike, so both are refused the same way.
 */
export function validateThumbnailSource(source: SourceSize): ThumbnailValidation<ThumbnailCropRejectionReason> {
  return validateThumbnailCrop(source, initialThumbnailCrop(source));
}

/** The exported JPEG's side: the crop's own size, capped, never upscaled. */
export function thumbnailExportSize(crop: ThumbnailCrop): number {
  return Math.min(Math.floor(crop.width), MAX_THUMBNAIL_EXPORT_PIXELS);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/** The crop slid by a drag, stopping at the source's edges. */
export function moveThumbnailCrop(source: SourceSize, crop: ThumbnailCrop, dx: number, dy: number): ThumbnailCrop {
  return {
    ...crop,
    x: Math.round(clamp(crop.x + dx, 0, Math.max(0, source.width - crop.width))),
    y: Math.round(clamp(crop.y + dy, 0, Math.max(0, source.height - crop.height))),
  };
}

/**
 * The crop resized to `side` around its own centre, no larger than the source's
 * short side, then pushed back inside the source if it overflows an edge. No
 * lower bound: a crop zoomed under the minimum is the validation's to refuse,
 * with a sentence, not the zoom's to swallow silently.
 */
export function zoomThumbnailCrop(source: SourceSize, crop: ThumbnailCrop, side: number): ThumbnailCrop {
  const nextSide = Math.round(clamp(side, 1, Math.min(source.width, source.height)));
  const centreX = crop.x + crop.width / 2;
  const centreY = crop.y + crop.height / 2;
  return moveThumbnailCrop(
    source,
    { x: centreX - nextSide / 2, y: centreY - nextSide / 2, width: nextSide, height: nextSide },
    0,
    0,
  );
}
