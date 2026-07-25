/**
 * Whether the stub Payment Provider's interstitial may exist on this
 * deployment.
 *
 * The guard is deliberately simple and honest: the interstitial renders
 * anywhere that is not a production build — which is exactly where the API
 * selects the stub provider (no PAYPHONE_* credentials, see
 * backend/internal/platform/payment.go) — plus any deployment that opts in
 * explicitly with STOREFRONT_STUB_PAYMENTS=1 (the production-parity stack,
 * which runs production builds against a stub-provider API). A real production
 * deployment sets neither, and /checkout/stub is a plain 404 there.
 *
 * The page is a dumb terminal either way: it can only render what the API's
 * stub provider put in its redirect URL, and its buttons only drive the
 * return-redirect legs. Reaching it without a pending Payment settles nothing.
 */
export function stubPaymentsActive(): boolean {
  return process.env.NODE_ENV !== "production" || process.env.STOREFRONT_STUB_PAYMENTS === "1";
}
