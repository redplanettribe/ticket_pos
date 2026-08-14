import type { MetadataRoute } from "next";

/**
 * Deliberately English, and deliberately outside the catalog — the same call the
 * Storefront's manifest makes, and it is bilingual too.
 *
 * A manifest is fetched by the browser as an install-time resource rather than
 * as a page: no session, and no promise that the cookie the locale ladder reads
 * is sent with it. A per-reader manifest would be a manifest whose language was
 * decided by whichever request happened to populate the cache. The name is the
 * product's own and reads as coined in both languages regardless.
 */
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Multiticketing Staff",
    short_name: "MT Staff",
    description: "Staff console for Multiticketing — manage organizations, events, and the point of sale.",
    start_url: "/",
    display: "standalone",
    background_color: "#ffffff",
    theme_color: "#0e84c1",
    icons: [
      { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
    ],
  };
}
