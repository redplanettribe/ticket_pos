# Cover Video: a verbatim MP4 with no transcoding pipeline

## Context

An Event's page sells with one hero surface, and today that surface is a still: the Cover
Image, uploaded by an Org Admin through a presigned PUT straight to the public bucket and
served back byte-for-byte. Organizers want motion there — a short clip that makes the page
feel alive — and the obvious modern answer is a video platform: upload anything, let Mux or
Cloudflare Stream or the GCS Transcoder chew it into adaptive HLS, and embed a player.

That answer buys real things — any input format, bitrate ladders, long content — and it
costs real things: the platform's first third-party media dependency, a new bill, webhooks,
and an asynchronous "processing" state on the Event where today every media attach is
synchronous and done the moment the PUT returns. It is also an answer to a question nobody
asked. The hero is not a place people watch; it is a moving cover image — muted, looping,
decorative, abandoned the moment the visitor scrolls to the ticket list.

## Decision

The **Cover Video** is a single H.264/AAC MP4, uploaded by presigned PUT to
`videos/{orgID}/{eventID}/`, stored verbatim, and served as a plain `<video>` source with
the Cover Image as its poster. No transcoding, no adaptive bitrate, no processing state:
the same one-key-column, one-presigned-PUT shape as covers, logos and avatars.

What a pipeline would have normalized, upload constraints refuse instead: roughly 16:9,
at least 1280 pixels wide, at most 30 seconds and 50 MB. The constraints are enforced in
the browser — the only place that can read a video's dimensions without the server growing
an ffprobe dependency, which would be the processing step this decision declines. That is
the same trust model as every existing media check: a determined organizer can bypass it,
and the blast radius is their own event page.

The posture is ambient by construction. Muted, looping, inline, no controls; the Cover
Image renders first and the video takes over when buffered. Only the Event page hero ever
fetches the file — Storefront listings and link previews always use the Cover Image — and
visitors who ask for reduced motion or data saving are never sent it at all. This is not
only taste: muted is the only autoplay browsers permit, so an ambient clip is the only
kind of hero video that plays.

The constraints are also the escalation tripwire. The day organizers need trailers —
minutes long, watched with sound, at real traffic — is the day the streaming-service
trade-off gets reopened, as a successor to this ADR. Nothing here blocks that migration;
the video is one column and one bucket prefix.

## Considered options

- **A video service (Mux, Cloudflare Stream, GCS Transcoder)** — correct for content
  people watch, rejected for content that decorates. It would make the platform's simplest
  media flow its most complex: an async pipeline, a webhook surface and a vendor bill, all
  to re-encode thirty muted seconds that a phone already exported as playable H.264.
- **Server-side inspection of the uploaded bytes** — ffprobe on attach, so the constraints
  bind rather than advise. Rejected because it reintroduces a processing step on the server
  to harden a limit whose violation harms only the violator's own page, and because no
  other media upload verifies its bytes either; the trust model stays uniform.
- **Roomier caps for real promo videos** — rejected because a two-minute, 200 MB file
  autoplaying from a CDN-less public bucket is hostile to visitors and to the egress bill,
  and because stretching verbatim MP4 toward trailer territory is exactly the misuse the
  caps exist to make visible.

## Consequences

**Every hero view downloads the whole file.** There is no bitrate ladder and no CDN; a
visitor on a slow connection fetches the same 50 MB ceiling as one on fiber, and egress
scales linearly with page views. The caps bound the damage; they do not remove it. The
first event page with real traffic makes this the cheapest ADR to revisit.

**The dimension rules are advisory in fact, mandatory in appearance.** They live only in
the browser, so an API-wielding organizer can attach a vertical, four-minute, oversized
file and the server will store and serve it. Accepted deliberately — the page it ruins is
theirs — but anyone auditing "validated uploads" should know where the validation lives.

**Replacing media now deletes the old object** — the first delete the storage layer has
ever performed, added because a 50 MB orphan per editing iteration is not the rounding
error a 5 MB one was. The database commits first and the delete is best-effort, logged and
never surfaced: a failed cleanup recreates yesterday's status quo, an orphan. Cover Images
gain the same cleanup in the same path; logos and avatars keep leaking until someone
touches them.
