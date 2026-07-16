# Add a minimal public Organization endpoint for storefront branding

Until now every backend route is staff-session-gated; the Storefront app doesn't call the backend at all and derives the Organization's display name by title-casing its slug. Adding a Logo to Organizations meant the Storefront needed real Organization data (name, slug, logo) to render branding, so we introduced `GET /api/v1/public/organizations/{slug}` — unauthenticated, read-only, returning only `{name, slug, logo_url}`.

Considered deferring this (add `logo_url` to the staff-authenticated model only, wire up the Storefront later) but decided the Storefront's branding need was immediate and the endpoint's surface is small enough to commit to now. Future public endpoints should follow this same shape: minimal fields, no internal IDs, no write access.
