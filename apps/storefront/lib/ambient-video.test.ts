import assert from "node:assert/strict";
import test from "node:test";

import { shouldPlayAmbientVideo } from "./ambient-video.ts";

const VIDEO_URL = "https://storage.example.com/videos/org/event/clip.mp4";

// Nobody has asked for less: the Event has a Cover Video and the visitor's
// browser reports neither preference, so the hero loads it.
test("a visitor who asked for nothing gets the Cover Video", () => {
  assert.equal(
    shouldPlayAmbientVideo(VIDEO_URL, { prefersReducedMotion: false, saveData: false }),
    true,
  );
});

// The accessibility opt-out. Not a paused video and not a shorter one — none at
// all, so no element is rendered and no bytes are fetched.
test("a reduced-motion visitor is never sent the Cover Video", () => {
  assert.equal(
    shouldPlayAmbientVideo(VIDEO_URL, { prefersReducedMotion: true, saveData: false }),
    false,
  );
});

// The metered-connection opt-out. The MP4 is served verbatim from a bucket with
// no bitrate ladder in front of it, so "load it smaller" is not an option the
// platform has (ADR 0020).
test("a data-saver visitor is never sent the Cover Video", () => {
  assert.equal(
    shouldPlayAmbientVideo(VIDEO_URL, { prefersReducedMotion: false, saveData: true }),
    false,
  );
});

test("both preferences together still refuse the Cover Video", () => {
  assert.equal(
    shouldPlayAmbientVideo(VIDEO_URL, { prefersReducedMotion: true, saveData: true }),
    false,
  );
});

// Most Events have no Cover Video, and the caller should not have to ask a
// second question to find that out: the answer is the same "no" the opt-outs
// produce, and so is the hero it leads to.
test("an Event with no Cover Video has nothing to play", () => {
  for (const missing of [null, undefined, ""]) {
    assert.equal(
      shouldPlayAmbientVideo(missing, { prefersReducedMotion: false, saveData: false }),
      false,
    );
  }
});
