// The Storefront's own public origin, injected at runtime as STOREFRONT_BASE_URL
// (see terraform/modules/ticket-pos/cloud_run_frontends.tf). It is a runtime —
// not NEXT_PUBLIC — variable so one built image serves every environment, the
// same treatment API_URL gets in lib/api.ts.
//
// Used only to set Next's metadataBase, from which canonical and og:url resolve
// to absolute URLs. Absent in local dev and preview stacks, where it is left
// undefined and Next simply emits relative URLs — og:image already carries an
// absolute object-storage URL, so link previews are unaffected either way.
export function storefrontBaseUrl(): URL | undefined {
  const raw = process.env.STOREFRONT_BASE_URL?.trim();
  if (!raw) return undefined;
  try {
    return new URL(raw);
  } catch {
    return undefined;
  }
}
