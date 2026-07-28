import assert from "node:assert/strict";
import test from "node:test";

import {
  ASPECT_TOLERANCE,
  MAX_COVER_VIDEO_BYTES,
  MAX_COVER_VIDEO_SECONDS,
  MIN_COVER_VIDEO_WIDTH,
  validateCoverVideo,
  type CoverVideoFile,
} from "./cover-video.ts";

/** A file that satisfies every rule, so each test can violate exactly one. */
function validFile(overrides: Partial<CoverVideoFile> = {}): CoverVideoFile {
  return {
    width: 1920,
    height: 1080,
    duration: 12,
    size: 8 * 1024 * 1024,
    type: "video/mp4",
    ...overrides,
  };
}

function assertRejected(file: CoverVideoFile, expected: string): string {
  const result = validateCoverVideo(file);
  assert.equal(result.ok, false, `expected ${JSON.stringify(file)} to be rejected`);
  if (result.ok) {
    throw new Error("unreachable");
  }
  assert.equal(result.reason, expected);
  assert.ok(result.message.length > 0, "a rejection must carry a human-readable message");
  return result.message;
}

function assertAccepted(file: CoverVideoFile) {
  assert.deepEqual(validateCoverVideo(file), { ok: true }, `expected ${JSON.stringify(file)} to pass`);
}

test("the shapes organizers actually export pass", () => {
  // 16:9 exactly, at and above the minimum width.
  assertAccepted(validFile({ width: 1920, height: 1080 }));
  assertAccepted(validFile({ width: 1280, height: 720 }));
  assertAccepted(validFile({ width: 1600, height: 900 }));
  assertAccepted(validFile({ width: 3840, height: 2160 }));
  assertAccepted(validFile({ width: 2560, height: 1440 }));
});

test("a vertical or squarish video is not a hero", () => {
  // The social-media cut every organizer has: 9:16.
  assertRejected(validFile({ width: 1080, height: 1920 }), "aspect");
  // 4:3 and 5:4 are inside no reasonable tolerance of 16:9 either.
  assertRejected(validFile({ width: 1440, height: 1080 }), "aspect");
  assertRejected(validFile({ width: 1600, height: 1280 }), "aspect");
  // Ultra-wide overshoots on the other side.
  assertRejected(validFile({ width: 2560, height: 1080 }), "aspect");
});

test("the aspect tolerance admits both sides and stops just past them", () => {
  const target = 16 / 9;
  const height = 1080;

  // Exactly at the tolerance, on both sides, still passes.
  assertAccepted(validFile({ width: height * target * (1 - ASPECT_TOLERANCE), height }));
  assertAccepted(validFile({ width: height * target * (1 + ASPECT_TOLERANCE), height }));

  // A hair beyond it, on both sides, does not.
  assertRejected(validFile({ width: height * target * (1 - ASPECT_TOLERANCE) - 1, height }), "aspect");
  assertRejected(validFile({ width: height * target * (1 + ASPECT_TOLERANCE) + 1, height }), "aspect");
});

test("the tolerance is loose enough for real off-by-a-pixel encodes", () => {
  // 1920x1088 (macroblock-padded) and 1918x1080 are 16:9 in every way that matters.
  assertAccepted(validFile({ width: 1920, height: 1088 }));
  assertAccepted(validFile({ width: 1918, height: 1080 }));
  // 1920x1200 (16:10) is a different shape and must not sneak through.
  assertRejected(validFile({ width: 1920, height: 1200 }), "aspect");
});

test("the minimum width binds at exactly 1280", () => {
  assertAccepted(validFile({ width: MIN_COVER_VIDEO_WIDTH, height: 720 }));
  assertRejected(validFile({ width: MIN_COVER_VIDEO_WIDTH - 1, height: 719 }), "width");
  // A correctly shaped but small clip is still too small.
  assertRejected(validFile({ width: 640, height: 360 }), "width");
});

test("the duration cap binds at exactly 30 seconds", () => {
  assertAccepted(validFile({ duration: MAX_COVER_VIDEO_SECONDS }));
  assertAccepted(validFile({ duration: 0.5 }));
  assertRejected(validFile({ duration: MAX_COVER_VIDEO_SECONDS + 0.01 }), "duration");
  assertRejected(validFile({ duration: 180 }), "duration");
});

test("the size cap binds at exactly 50 MB", () => {
  assert.equal(MAX_COVER_VIDEO_BYTES, 50 * 1024 * 1024);
  assertAccepted(validFile({ size: MAX_COVER_VIDEO_BYTES }));
  assertRejected(validFile({ size: MAX_COVER_VIDEO_BYTES + 1 }), "size");
});

test("only MP4 is accepted", () => {
  assertAccepted(validFile({ type: "video/mp4" }));
  for (const type of ["video/quicktime", "video/webm", "image/png", "application/octet-stream", ""]) {
    assertRejected(validFile({ type }), "type");
  }
});

test("metadata the browser could not read is rejected rather than passed", () => {
  // A file the video element never resolved: zeros, or a live stream's Infinity.
  assertRejected(validFile({ width: 0, height: 0 }), "unreadable");
  assertRejected(validFile({ width: 1920, height: 0 }), "unreadable");
  assertRejected(validFile({ width: Number.NaN, height: Number.NaN }), "unreadable");
  assertRejected(validFile({ duration: Number.NaN }), "unreadable");
  assertRejected(validFile({ duration: Number.POSITIVE_INFINITY }), "unreadable");
  assertRejected(validFile({ duration: 0 }), "unreadable");
  assertRejected(validFile({ size: 0 }), "unreadable");
  assertRejected(validFile({ width: -1920, height: -1080 }), "unreadable");
});

test("the wrong type is reported before unreadable metadata, so the fixable thing is said first", () => {
  // A PNG has no video metadata at all; the useful message is "MP4 only".
  assertRejected({ width: 0, height: 0, duration: Number.NaN, size: 1024, type: "image/png" }, "type");
});

test("every rejection message states the requirement it violated", () => {
  assert.match(assertRejected(validFile({ type: "video/webm" }), "type"), /MP4/i);
  assert.match(assertRejected(validFile({ size: MAX_COVER_VIDEO_BYTES + 1 }), "size"), /50 MB/i);
  assert.match(assertRejected(validFile({ duration: 45 }), "duration"), /30 seconds/i);
  assert.match(assertRejected(validFile({ width: 640, height: 360 }), "width"), /1280/);
  assert.match(assertRejected(validFile({ width: 1080, height: 1920 }), "aspect"), /16:9/);
});
