/**
 * Where the "Create an event" invitation goes — the Storefront's one link out
 * of itself and into the staff app.
 *
 * The target is the staff sign-in page carrying an intent hint, rather than the
 * create-organization screen it eventually leads to. That is deliberate: the
 * staff middleware clears both path and query when it bounces an
 * unauthenticated request to sign-in, so a deep link arrives stripped of its
 * hint, whereas sign-in is a public path that returns before any clearing and
 * therefore keeps it. The existing auth fork carries a Membership-less session
 * onwards from there.
 *
 * STAFF_BASE_URL is a runtime — not NEXT_PUBLIC — variable, the same treatment
 * STOREFRONT_BASE_URL gets in lib/site.ts and API_URL in lib/api.ts, so one
 * built image serves every environment. That matters more here than elsewhere:
 * a hardcoded or defaulted origin would mean a local or staging Storefront
 * pointing at production staff, where a developer following the journey signs
 * in against real data and mints a real Staff Session.
 *
 * Every unusable value returns undefined rather than throwing or guessing, and
 * the footer then renders no link at all. The failure mode of a missing
 * variable is a missing invitation, never a broken one — which is the whole
 * reason this is a module rather than an inline expression at the call site.
 */
export function createEventCtaHref(): string | undefined {
  const raw = process.env.STAFF_BASE_URL?.trim();
  if (!raw) return undefined;
  try {
    // Resolved against the configured origin, so a trailing slash on the
    // variable — the likeliest way for two environments to disagree about the
    // same address — cannot produce "//login".
    const href = new URL("/login", raw);
    href.searchParams.set("intent", "create");
    return href.toString();
  } catch {
    return undefined;
  }
}
