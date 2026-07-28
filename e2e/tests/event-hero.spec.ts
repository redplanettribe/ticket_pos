import { test, expect } from "@playwright/test";

// The Storefront Event page hero with a Cover Video (issue #135), against the
// dev stack (`make dev`). Thin and attribute-level by design: what is asserted
// here is what crosses runtimes — the Go API's computed video URL reaching a
// `<video>` element the browser actually created, and the two opt-outs that
// decide whether that element exists at all. Real playback, buffering and the
// fade are not assertable from a headless browser with no bytes behind the URL;
// they live in docs/manual-verification-cover-video.md.
//
// Data: the dev-seed migration backend/migrations/034_seed_dev_cover_media.sql
// attaches placeholder Cover Image and Cover Video keys to the seeded Sunrise
// Jazz Brunch Event. The objects behind those keys do not exist, so the media
// 404s — which changes nothing here, because the element, its src and its
// poster are decided before a single byte is fetched.

const EVENT_PATH = "/demo-venue/events/sunrise-jazz-brunch";
const EVENT_NAME = "Sunrise Jazz Brunch";

// The seeded keys, as the API computes them into public bucket URLs.
const COVER_VIDEO_KEY =
  "videos/a0000000-0000-4000-8000-000000000001/c0000000-0000-4000-8000-000000000002/e0000000-0000-4000-8000-000000000002.mp4";
const COVER_IMAGE_KEY =
  "covers/a0000000-0000-4000-8000-000000000001/c0000000-0000-4000-8000-000000000002/e0000000-0000-4000-8000-000000000001.jpg";

test("the hero plays the Cover Video over the Cover Image poster", async ({ page }) => {
  await page.goto(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();

  // The element is created on the client, after mount, so it is awaited rather
  // than read once — this is exactly the ordering the feature promises: image
  // first from the server, video afterwards.
  const video = page.locator("video");
  await expect(video).toHaveCount(1);

  await expect(video).toHaveAttribute("src", new RegExp(`${COVER_VIDEO_KEY}$`));
  // The poster is the Cover Image: the video is a moving cover image and never
  // a hero of its own (ADR 0020).
  await expect(video).toHaveAttribute("poster", new RegExp(`${COVER_IMAGE_KEY}$`));

  // Ambient posture. Muted is what makes autoplay legal at all; playsInline is
  // what stops a phone from opening a fullscreen player; controls are absent
  // because there is nothing here to watch on purpose.
  await expect(video).toHaveJSProperty("muted", true);
  await expect(video).toHaveJSProperty("loop", true);
  await expect(video).toHaveJSProperty("autoplay", true);
  await expect(video).toHaveJSProperty("playsInline", true);
  await expect(video).toHaveJSProperty("controls", false);

  // The Cover Image is under it the whole time, so a video that never buffers
  // leaves a complete hero rather than a hole.
  await expect(page.locator(`img[src$="${COVER_IMAGE_KEY}"]`)).toHaveCount(1);
});

test("a reduced-motion visitor is sent no video at all", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();

  // No element, which is the point: not a paused video, not a hidden one. The
  // still Cover Image is the entire hero, exactly as before Cover Videos
  // existed. The image assertion is what proves the hero rendered at all, so an
  // empty page could not pass this test by having no video in it.
  await expect(page.locator(`img[src$="${COVER_IMAGE_KEY}"]`)).toHaveCount(1);
  await expect(page.locator("video")).toHaveCount(0);
});

test("Storefront listing cards stay still images", async ({ page }) => {
  // The global explorer lists the same Event whose hero carries a video. Cards
  // are scanned, not watched, and a grid of autoplaying loops is the thing this
  // decision exists to prevent.
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 3, name: EVENT_NAME })).toBeVisible();
  await expect(page.locator("video")).toHaveCount(0);

  // The Organization page lists it too, from the same card component.
  await page.goto("/demo-venue");
  await expect(page.getByRole("heading", { level: 3, name: EVENT_NAME })).toBeVisible();
  await expect(page.locator("video")).toHaveCount(0);
});
