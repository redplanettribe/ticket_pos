import type { MetadataRoute } from "next";

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
