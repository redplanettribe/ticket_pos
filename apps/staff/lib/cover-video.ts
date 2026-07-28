/**
 * Cover Video upload constraints (ADR 0020).
 *
 * The Cover Video is stored verbatim — no transcoding, no server-side byte
 * inspection — so the browser is the only place these rules are ever applied.
 * This function is that place: everything the staff app knows about a chosen
 * file (its type and size from the File, its dimensions and duration from a
 * video element's loaded metadata) goes in, and either an approval or one
 * rejection carrying the requirement it violated comes out.
 *
 * A determined organizer can bypass it, and the page they ruin is their own.
 */

/** Half the tolerated deviation band around 16:9 — 3% either side. */
export const ASPECT_TOLERANCE = 0.03;

/** The target hero shape: landscape 16:9. */
export const TARGET_ASPECT_RATIO = 16 / 9;

/** Narrower than this and the hero looks soft on a desktop viewport. */
export const MIN_COVER_VIDEO_WIDTH = 1280;

/** An ambient loop, not a trailer. */
export const MAX_COVER_VIDEO_SECONDS = 30;

/** The egress ceiling: every hero view downloads the whole file. */
export const MAX_COVER_VIDEO_BYTES = 50 * 1024 * 1024;

/** The one content type the upload-URL endpoint allowlists. */
export const COVER_VIDEO_CONTENT_TYPE = "video/mp4";

/** What the staff app can observe about a chosen file before uploading it. */
export type CoverVideoFile = {
  /** `videoWidth` in pixels, from the loaded metadata. */
  width: number;
  /** `videoHeight` in pixels, from the loaded metadata. */
  height: number;
  /** `duration` in seconds, from the loaded metadata. */
  duration: number;
  /** `File.size` in bytes. */
  size: number;
  /** `File.type`, the browser-sniffed content type. */
  type: string;
};

/** Which requirement a file failed. */
export type CoverVideoRejectionReason = "type" | "unreadable" | "size" | "width" | "aspect" | "duration";

export type CoverVideoValidation =
  | { ok: true }
  | { ok: false; reason: CoverVideoRejectionReason; message: string };

function reject(reason: CoverVideoRejectionReason, message: string): CoverVideoValidation {
  return { ok: false, reason, message };
}

/** A positive, finite measurement — anything else means metadata never resolved. */
function measured(value: number): boolean {
  return Number.isFinite(value) && value > 0;
}

/**
 * The chosen file against every Cover Video rule, in the order that gives the
 * organizer the most actionable message: a non-MP4 has no video metadata worth
 * complaining about, and a file whose metadata never loaded cannot be measured
 * against the dimension rules at all.
 */
export function validateCoverVideo(file: CoverVideoFile): CoverVideoValidation {
  if (file.type !== COVER_VIDEO_CONTENT_TYPE) {
    return reject("type", "The cover video must be an MP4 file.");
  }

  if (!measured(file.size) || !measured(file.width) || !measured(file.height) || !measured(file.duration)) {
    return reject(
      "unreadable",
      "We could not read this video. The cover video must be an MP4 that is landscape 16:9, at least 1280 pixels wide, 30 seconds or shorter, and 50 MB or smaller.",
    );
  }

  if (file.size > MAX_COVER_VIDEO_BYTES) {
    return reject("size", "The cover video must be 50 MB or smaller.");
  }

  // Shape before size: the vertical phone cut every organizer has is also
  // narrower than the minimum, and "make it landscape" is the useful advice.
  const ratio = file.width / file.height;
  const deviation = Math.abs(ratio - TARGET_ASPECT_RATIO) / TARGET_ASPECT_RATIO;
  // The epsilon is floating-point slack, not extra tolerance: a shape computed
  // as exactly 3% off must land inside the band rather than a bit outside it.
  if (deviation > ASPECT_TOLERANCE + 1e-9) {
    return reject(
      "aspect",
      "The cover video must be landscape 16:9 (for example 1920x1080 or 1280x720).",
    );
  }

  if (file.width < MIN_COVER_VIDEO_WIDTH) {
    return reject("width", `The cover video must be at least ${MIN_COVER_VIDEO_WIDTH} pixels wide.`);
  }

  if (file.duration > MAX_COVER_VIDEO_SECONDS) {
    return reject("duration", `The cover video must be ${MAX_COVER_VIDEO_SECONDS} seconds or shorter.`);
  }

  return { ok: true };
}
