import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Multiticketing",
    short_name: "Multiticketing",
    description: "Discover and buy tickets to events from organizers everywhere.",
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
