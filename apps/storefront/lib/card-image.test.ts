/**
 * Which image a card shows (CONTEXT.md: Thumbnail, Cover Image): the one rule
 * behind every Event card's frame, asserted here so the fallback cannot drift
 * between card shapes (spec #853).
 */

import assert from "node:assert/strict";
import test from "node:test";

// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import { cardImageUrl } from "./card-image.ts";

const thumbnail = "https://media.example/thumbnails/o/e/square.jpg";
const cover = "https://media.example/covers/o/e/wide.jpg";

test("a square frame shows the Thumbnail when the Event has one", () => {
  assert.equal(cardImageUrl({ thumbnail_url: thumbnail, cover_image_url: cover }, "square"), thumbnail);
  assert.equal(cardImageUrl({ thumbnail_url: thumbnail, cover_image_url: null }, "square"), thumbnail);
});

test("a square frame falls back to the Cover Image when there is no Thumbnail", () => {
  assert.equal(cardImageUrl({ thumbnail_url: null, cover_image_url: cover }, "square"), cover);
});

test("a frame with neither image gets the stand-in", () => {
  assert.equal(cardImageUrl({ thumbnail_url: null, cover_image_url: null }, "square"), null);
  assert.equal(cardImageUrl({ thumbnail_url: null, cover_image_url: null }, "wide"), null);
});

test("a wide frame shows the Cover Image and ignores the Thumbnail", () => {
  assert.equal(cardImageUrl({ thumbnail_url: thumbnail, cover_image_url: cover }, "wide"), cover);
  // Never a square stretched into a wide frame: no Cover Image means the stand-in.
  assert.equal(cardImageUrl({ thumbnail_url: thumbnail, cover_image_url: null }, "wide"), null);
});
