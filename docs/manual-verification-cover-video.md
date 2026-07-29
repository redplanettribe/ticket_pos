# Manual verification: Cover Video hero

Run this **whenever the Event page hero or the Cover Video upload path is touched**, and once
before go-live for the feature. It covers what no automated layer can.

The Playwright spec (`e2e/tests/event-hero.spec.ts`) proves the hero's *decisions*: that a
`<video>` element is created with the right `src` and `poster`, that it is muted, looping,
inline, autoplaying and control-free, that reduced motion produces no element at all, and that
listing cards stay still. It proves all of that against a placeholder key with **no bytes behind
it**, because attributes are decided before a byte is fetched.

What it cannot prove is everything that only exists once real video is really playing: that an
organizer's MP4 decodes and *loops seamlessly*, that the fade actually looks like a cross-fade
rather than a flash, that a phone plays it in the page instead of hijacking the screen, and — the
two this runbook exists for — that a **data-saver** visitor and a visitor on a **slow connection**
get what ADR 0020 promises them: nothing at all, and a still hero until the video is genuinely
ready.

Cost while testing: the video is served verbatim from a CDN-less bucket, so every hero view
downloads the whole file (ADR 0020). Reloading section 5 fifty times costs fifty full downloads.
That is the consequence being verified, not a mistake.

## Prerequisites

- The dev stack (`make dev`) with the seeded catalog, and Staff signed in as an **Org Admin** of
  the Demo Venue Organization
- **A real MP4 within the caps**: roughly 16:9, at least 1280 px wide, at most 30 s and 50 MB,
  H.264/AAC. Any phone-exported clip qualifies. A clip with obvious motion (not a near-still) is
  worth choosing — a fade into a static frame is invisible
- A second, deliberately **large** MP4 near the 50 MB cap for section 5. If you have only a small
  one, section 5's throttling still works; the wait is just shorter
- The dev seed (migration 034) attaches placeholder keys to **Sunrise Jazz Brunch** whose objects
  do not exist. Every pass below uses an Event you upload real media to, so use **Midnight Synth
  Live** and upload both a Cover Image and a Cover Video to it in Staff first
- Chrome or Edge for section 4: **data saver is Chromium-only**, and so is `navigator.connection`

## 1. Playback and looping (desktop)

Open `http://localhost:64300/demo-venue/events/midnight-synth-live`.

1. The hero plays: moving picture, immediately, with no click
2. **No sound at any volume.** Unmute nothing — there are no controls to unmute with, and that is
   the assertion: no play button, no scrubber, no fullscreen affordance, no context of a player
3. Watch it past the end of the clip **twice**. It loops, and the loop does not stall, flash black
   or restart with a visible seek. A hitch here is usually the file (a long GOP or a stray audio
   track), not the page — try a second MP4 before filing anything
4. Scroll to the ticket list and back. The video is still playing and has not restarted from a
   paused state
5. Right-click the hero. The browser offers no video menu of consequence — nothing here invites
   being treated as a watchable asset

## 2. The fade (the whole point of the polish)

Reload with the network tab open and **cache disabled**.

1. The **Cover Image is visible in the very first paint**. There is no blank box, no muted-grey
   rectangle, no spinner, and no letterboxed black — not for one frame. If you catch a flash of
   anything but the image, that is the bug this section is for
2. The video then **fades in** over the image rather than cutting to it. Watch the transition
   itself: a hard swap looks like a flicker even when it is fast
3. Confirm it fades in at the moment it is *ready to run through*, not the moment the first frame
   decodes: the `canplaythrough` event in the Performance/Network panel should sit just before the
   opacity change, and the video should never stutter in its first second
4. Throttle to **Fast 3G** and reload. The image holds visibly longer, then the fade happens. The
   hero is complete and readable the entire time
5. Reload while the page is **scrolled past the hero** (deep link with the ticket list in view,
   then scroll up). The hero has not popped, resized or shifted the layout — the aspect-ratio box
   is fixed, so nothing below it moves

## 3. Failure stays invisible

1. In dev tools, block the video request (Network → block request URL) and reload. Assert: the
   still Cover Image hero, permanently, with **no error, no broken-media icon, no empty box** —
   from the visitor's side nothing has failed
2. Point an Event's video key at a nonexistent object (the seeded **Sunrise Jazz Brunch** already
   is one) and open its page. Same outcome: still hero, 404 in the network tab only
3. Confirm the page's other surfaces are unaffected: heading, date, venue, ticket list, checkout

## 4. Data saver and reduced motion

Both of these are "the request is never made". The network tab is the assertion, not the pixels.

1. **Reduced motion.** Turn the OS setting on (GNOME: *Settings → Accessibility → Reduce
   Animation*; macOS: *Accessibility → Display → Reduce motion*), reload with an empty network tab
   filtered to Media. Assert: the still Cover Image, **no `<video>` element** in the Elements panel
   at all, and **no request for the `.mp4`** — not a cancelled one, not a partial one, none
2. Turn it off, reload, and confirm the video comes back — the opt-out must not be sticky
3. **Data saver.** In Chrome, enable the data-saver signal (a Lite-mode extension, or DevTools
   console `navigator.connection` overridden before load; on Android Chrome the real setting is
   under *Settings → Data Saver*). Reload and assert the same three things: still image, no
   element, no request
4. Verify the signal was actually on — `navigator.connection.saveData` in the console must read
   `true`. A pass with the signal off proves nothing, and this is the easiest step in the runbook
   to fool yourself with
5. On both passes, confirm the **listing cards and the Organization page look identical** to a
   normal visit: they never had video to withhold

## 5. Slow network (the image holds)

With the **large** MP4 attached:

1. Throttle to **Slow 3G**, disable cache, reload
2. Assert the Cover Image is up and readable for the whole download, and that the hero is *usable*
   — the title, date and ticket list are all interactive while the video is still arriving
3. Assert the video does **not** fade in early and stutter. Under `canplaythrough` it should
   appear once, playing smoothly, or — if the connection is bad enough — not at all, which is a
   perfectly good outcome
4. Note the transferred size in the network tab. This is the per-view egress ADR 0020 accepts, and
   the number worth quoting the day the decision is revisited

## 6. Mobile inline (iOS is the one that matters)

Use a real phone against the dev machine's LAN address, or the parity stack. Simulators do not
reproduce iOS Safari's fullscreen behavior reliably.

1. **iOS Safari**: the video plays **in the page**. It must not open the fullscreen player, and no
   playback chrome may appear over the hero. A fullscreen takeover here means `playsInline` was
   lost — the single most fragile attribute in this feature
2. Scroll and rotate. The hero keeps its 16:9 box, the video stays inline, and nothing hijacks the
   screen
3. **iOS Low Power Mode**: iOS suspends autoplay. Assert the hero degrades to the **still poster**
   and never to a black box with a play button in the middle
4. **Android Chrome**: plays inline; then enable **Data Saver** in Chrome's settings and reload —
   no video element, no request, per section 4
5. On both, confirm the page's scroll performance is unaffected and the device does not warm
   noticeably — a 30 s ambient loop should not behave like a video player

## 7. Everywhere else is still an image

One pass to confirm the video stayed where it was put:

1. The global explorer (`/`) and the Organization page (`/demo-venue`): the Event's card shows the
   **Cover Image**. No video element, no `.mp4` request on either page
2. Paste the event URL into a link-preview checker (or read the page source): `og:image` and
   `twitter:image` are the **Cover Image URL**, and no video URL appears in the metadata at all
3. The Staff event detail page still previews the video for an Org Admin, and an Event Owner who
   is not an Org Admin sees it without upload controls

## Rollback

The hero is the only surface that ever loads a Cover Video, and one component decides it:
`EventHeroMedia` returning before the `<video>` branch — or `shouldPlayAmbientVideo` in
`apps/storefront/lib/ambient-video.ts` returning `false` — removes video from the Storefront
entirely while leaving the still Cover Image hero untouched. Uploaded videos stay attached and
stored; nothing to unwind, and re-enabling brings them back.
