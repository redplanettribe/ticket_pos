"use client";

import { useEffect, useState } from "react";

import { cn } from "@ticket-pos/ui";

import { ambientVideoSignals, shouldPlayAmbientVideo } from "@/lib/ambient-video";

/**
 * The Event page hero's moving picture — the one surface on the Storefront that
 * ever fetches a Cover Video.
 *
 * The Cover Image is the hero. It renders first, from the server, on every
 * request, and it never goes away: the video is layered over it and fades in
 * only once it can play through, so the hero is complete from the first paint
 * and is never blank, letterboxed or spinning. When the video is refused, slow,
 * or broken, what remains is exactly the still hero this page had before the
 * Cover Video existed — which is why the image is not conditional on anything.
 *
 * Being a Client Component is the point rather than an implementation detail.
 * Whether to load the video at all depends on two things only the browser knows
 * — reduced motion and data saver (see lib/ambient-video) — so the decision
 * cannot be made while rendering on the server. The server therefore renders the
 * image alone, and the `<video>` element is created after mount or not at all:
 * a visitor who declined it never receives a `src` to fetch, not even one the
 * browser is told to ignore.
 *
 * The posture is ambient by construction (ADR 0020): muted, looping, inline,
 * autoplaying, no controls. Muted is not a stylistic choice — it is the only
 * autoplay browsers permit — and `playsInline` is what stops iOS Safari from
 * taking the clip fullscreen and turning a decoration into a player.
 */

type EventHeroMediaProps = {
  /** The Cover Image URL. Required: it is the poster and the fallback both. */
  coverImageUrl: string;
  /** The Cover Video URL, when the Event has one. */
  coverVideoUrl: string | null;
};

export function EventHeroMedia({ coverImageUrl, coverVideoUrl }: EventHeroMediaProps) {
  // Both start false so the server's HTML and the first client render agree:
  // image only. Nothing about the video is decided until the effect runs.
  const [playVideo, setPlayVideo] = useState(false);
  const [videoVisible, setVideoVisible] = useState(false);

  useEffect(() => {
    // Decided once, on mount. A visitor who flips the system preference while
    // reading an event page is not worth a live subscription here: the next
    // navigation re-reads it, and tearing the video out mid-fade would itself
    // be motion.
    setPlayVideo(shouldPlayAmbientVideo(coverVideoUrl, ambientVideoSignals()));
  }, [coverVideoUrl]);

  return (
    <>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={coverImageUrl} alt="" className="h-full w-full object-cover" />
      {playVideo && coverVideoUrl ? (
        <video
          src={coverVideoUrl}
          poster={coverImageUrl}
          muted
          loop
          playsInline
          autoPlay
          preload="metadata"
          aria-hidden="true"
          // `canplaythrough` and not `canplay`: the promise this hero makes is
          // that the video takes over when it can run, not when it can start.
          // Fading in on the first decodable frame is how a slow connection
          // gets a hero that stutters or freezes on frame one.
          onCanPlayThrough={() => setVideoVisible(true)}
          // A 404, a codec the browser will not decode, a dropped connection:
          // every one of them ends here, and every one of them means the still
          // Cover Image stays up. There is no error state to show, because from
          // the visitor's side nothing has failed.
          onError={() => setVideoVisible(false)}
          className={cn(
            // Layered over the image inside the hero's aspect-ratio box, so the
            // fade is a cross-fade between two identically framed pictures.
            "absolute inset-0 h-full w-full object-cover transition-opacity",
            videoVisible ? "opacity-100" : "opacity-0",
          )}
        />
      ) : null}
    </>
  );
}
