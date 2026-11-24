import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  MAX_THUMBNAIL_EXPORT_PIXELS,
  MAX_THUMBNAIL_FILE_BYTES,
  MIN_THUMBNAIL_CROP_PIXELS,
  initialThumbnailCrop,
  moveThumbnailCrop,
  thumbnailRejectionKey,
  thumbnailExportSize,
  validateThumbnailCrop,
  validateThumbnailFile,
  validateThumbnailSource,
  zoomThumbnailCrop,
  type ThumbnailCropRejectionReason,
  type ThumbnailRejectionReason,
  type ThumbnailFileRejectionReason,
} from "./thumbnail-crop.ts";

test("the initial crop of a landscape source is the full-height square in the middle", () => {
  assert.deepEqual(initialThumbnailCrop({ width: 1920, height: 1080 }), {
    x: 420,
    y: 0,
    width: 1080,
    height: 1080,
  });
});

test("the initial crop of a portrait source is the full-width square in the middle", () => {
  assert.deepEqual(initialThumbnailCrop({ width: 1080, height: 1350 }), {
    x: 0,
    y: 135,
    width: 1080,
    height: 1080,
  });
});

test("the initial crop of a square source is the whole source", () => {
  assert.deepEqual(initialThumbnailCrop({ width: 800, height: 800 }), {
    x: 0,
    y: 0,
    width: 800,
    height: 800,
  });
});

test("the initial crop lands on whole pixels when the spare space is odd", () => {
  const crop = initialThumbnailCrop({ width: 1001, height: 600 });
  assert.equal(crop.width, 600);
  assert.ok(Number.isInteger(crop.x));
  assert.ok(crop.x + crop.width <= 1001);
});

test("the initial crop is valid for any source at least 400 pixels on its short side", () => {
  for (const source of [
    { width: 400, height: 400 },
    { width: 4000, height: 400 },
    { width: 400, height: 6000 },
    { width: 3024, height: 4032 },
  ]) {
    assert.deepEqual(validateThumbnailCrop(source, initialThumbnailCrop(source)), { ok: true });
  }
});

function assertCropRejected(
  source: { width: number; height: number },
  crop: { x: number; y: number; width: number; height: number },
  reason: ThumbnailCropRejectionReason,
) {
  assert.deepEqual(validateThumbnailCrop(source, crop), { ok: false, reason });
}

test("a crop under 400 source pixels per side is refused", () => {
  assert.equal(MIN_THUMBNAIL_CROP_PIXELS, 400);
  const source = { width: 2000, height: 2000 };
  assert.deepEqual(validateThumbnailCrop(source, { x: 0, y: 0, width: 400, height: 400 }), { ok: true });
  assertCropRejected(source, { x: 0, y: 0, width: 399, height: 399 }, "small");
  // A source too small to hold a 400 pixel square fails on its initial crop.
  const tiny = { width: 640, height: 360 };
  assertCropRejected(tiny, initialThumbnailCrop(tiny), "small");
});

test("a crop reaching outside the source is refused", () => {
  const source = { width: 1000, height: 800 };
  assertCropRejected(source, { x: -1, y: 0, width: 500, height: 500 }, "outside");
  assertCropRejected(source, { x: 0, y: -1, width: 500, height: 500 }, "outside");
  assertCropRejected(source, { x: 501, y: 0, width: 500, height: 500 }, "outside");
  assertCropRejected(source, { x: 0, y: 301, width: 500, height: 500 }, "outside");
  // Flush with the far edges is still inside.
  assert.deepEqual(validateThumbnailCrop(source, { x: 500, y: 300, width: 500, height: 500 }), { ok: true });
});

test("a crop that is not square is refused", () => {
  assertCropRejected({ width: 2000, height: 2000 }, { x: 0, y: 0, width: 800, height: 600 }, "square");
});

test("a crop over an unreadable source is refused as too small, never passed", () => {
  assertCropRejected(
    { width: Number.NaN, height: Number.NaN },
    { x: 0, y: 0, width: Number.NaN, height: Number.NaN },
    "small",
  );
});

test("a source whose short side holds a 400 pixel square is accepted", () => {
  assert.deepEqual(validateThumbnailSource({ width: 1920, height: 1080 }), { ok: true });
  assert.deepEqual(validateThumbnailSource({ width: MIN_THUMBNAIL_CROP_PIXELS, height: 2000 }), { ok: true });
});

test("a source too small for a 400 pixel square, or unreadable, is refused as small", () => {
  assert.deepEqual(validateThumbnailSource({ width: 1920, height: MIN_THUMBNAIL_CROP_PIXELS - 1 }), {
    ok: false,
    reason: "small",
  });
  assert.deepEqual(validateThumbnailSource({ width: 0, height: 0 }), { ok: false, reason: "small" });
});

test("the export is the crop's own size, capped at 1080 and never upscaled", () => {
  assert.equal(MAX_THUMBNAIL_EXPORT_PIXELS, 1080);
  assert.equal(thumbnailExportSize({ x: 0, y: 0, width: 400, height: 400 }), 400);
  assert.equal(thumbnailExportSize({ x: 0, y: 0, width: 750, height: 750 }), 750);
  assert.equal(thumbnailExportSize({ x: 0, y: 0, width: 1080, height: 1080 }), 1080);
  assert.equal(thumbnailExportSize({ x: 0, y: 0, width: 1081, height: 1081 }), 1080);
  assert.equal(thumbnailExportSize({ x: 0, y: 0, width: 4032, height: 4032 }), 1080);
});

function assertFileRejected(file: { type: string; size: number }, reason: ThumbnailFileRejectionReason) {
  assert.deepEqual(validateThumbnailFile(file), { ok: false, reason });
}

test("JPEG, PNG and WebP are accepted, anything else is refused", () => {
  for (const type of ["image/jpeg", "image/png", "image/webp"]) {
    assert.deepEqual(validateThumbnailFile({ type, size: 1024 }), { ok: true });
  }
  for (const type of ["image/gif", "image/svg+xml", "image/heic", "image/avif", "video/mp4", ""]) {
    assertFileRejected({ type, size: 1024 }, "type");
  }
});

test("the file size limit binds at exactly 5 MB", () => {
  assert.equal(MAX_THUMBNAIL_FILE_BYTES, 5 * 1024 * 1024);
  assert.deepEqual(validateThumbnailFile({ type: "image/png", size: MAX_THUMBNAIL_FILE_BYTES }), { ok: true });
  assertFileRejected({ type: "image/png", size: MAX_THUMBNAIL_FILE_BYTES + 1 }, "size");
});

test("an unsupported type is reported before its size", () => {
  assertFileRejected({ type: "image/gif", size: MAX_THUMBNAIL_FILE_BYTES * 2 }, "type");
});

test("dragging the crop moves it and stops at the source's edges", () => {
  const source = { width: 1920, height: 1080 };
  const crop = { x: 420, y: 0, width: 600, height: 600 };
  assert.deepEqual(moveThumbnailCrop(source, crop, 100, 50), { x: 520, y: 50, width: 600, height: 600 });
  assert.deepEqual(moveThumbnailCrop(source, crop, -10000, -10000), { x: 0, y: 0, width: 600, height: 600 });
  assert.deepEqual(moveThumbnailCrop(source, crop, 10000, 10000), { x: 1320, y: 480, width: 600, height: 600 });
});

test("zooming keeps the crop square, centred where it was, and inside the source", () => {
  const source = { width: 1920, height: 1080 };
  const crop = { x: 660, y: 240, width: 600, height: 600 };
  // Zoom in around the same centre (960, 540).
  assert.deepEqual(zoomThumbnailCrop(source, crop, 400), { x: 760, y: 340, width: 400, height: 400 });
  // Zoom out as far as the source allows: the short side.
  const widest = zoomThumbnailCrop(source, crop, 5000);
  assert.deepEqual(widest, { x: 420, y: 0, width: 1080, height: 1080 });
  // Zooming out near an edge pushes the crop back inside rather than overflowing.
  const cornered = zoomThumbnailCrop(source, { x: 0, y: 0, width: 400, height: 400 }, 800);
  assert.deepEqual(cornered, { x: 0, y: 0, width: 800, height: 800 });
  for (const result of [widest, cornered]) {
    assert.deepEqual(validateThumbnailCrop(source, result), { ok: true });
  }
});

type Catalog = {
  event: Record<string, string>;
  errors: { envelope: Record<string, string> };
};

function catalog(locale: string): Catalog {
  return JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  ) as Catalog;
}

test("every rejection has a sentence to be said in, in both languages", () => {
  const reasons: ThumbnailRejectionReason[] = ["type", "size", "small", "outside", "square"];

  for (const locale of ["en", "es"]) {
    const { event } = catalog(locale);
    for (const reason of reasons) {
      assert.ok(event[thumbnailRejectionKey(reason)], `${locale}.json is missing ${thumbnailRejectionKey(reason)}`);
    }
  }
});

test("the API's Thumbnail content-type code is cataloged beside its media siblings", () => {
  // Presence only, in errors.envelope next to INVALID_IMAGE_CONTENT_TYPE and
  // INVALID_VIDEO_CONTENT_TYPE, as #867 asks. Like theirs, this copy is not
  // rendered yet: the API sends the code as a field error on content_type under
  // VALIDATION_FAILED, and the card resolves only the envelope code.
  for (const locale of ["en", "es"]) {
    assert.ok(
      catalog(locale).errors.envelope.INVALID_THUMBNAIL_CONTENT_TYPE,
      `${locale}.json is missing errors.envelope.INVALID_THUMBNAIL_CONTENT_TYPE`,
    );
  }
});
