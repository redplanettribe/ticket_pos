import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Multiticketing",
    short_name: "Multiticketing",
    description: "Discover and buy tickets to events from organizers everywhere.",
    // Deliberately locale-free, like the manifest's own address. An installed
    // app opening "/" is asking the same question a fresh visitor asks, and it
    // gets the same answer: a redirect into whichever Locale that browser's
    // cookie and languages name (middleware.ts).
    start_url: "/",
    display: "standalone",
    background_color: "#ffffff",
    // Neutral, not brand blue: the Storefront foregrounds Organization branding.
    theme_color: "#ffffff",
    icons: [
      { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
    ],
  };
}
