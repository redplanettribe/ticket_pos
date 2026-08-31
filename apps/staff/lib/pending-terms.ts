/**
 * The pending-terms cookie: how the Google door hands the terms step to the
 * login page (#538).
 *
 * The passcode path never needs it — the verify response lands in the form,
 * token and all. The Google callback is a redirect with no page to hand
 * anything to, so the single-use token rides an httpOnly cookie to /login and
 * from there, server-side, into the accept route. httpOnly because no script
 * has any business reading a credential: the form only needs to know a step is
 * pending, which the page tells it as a prop.
 *
 * Fifteen minutes, matching the token's own life on the API; a cookie that
 * outlives the row it names is a stale credential.
 */
const PENDING_TERMS = "ticket_pos_pending_terms";

export function pendingTermsCookieOptions() {
  const secure = process.env.NODE_ENV === "production";
  return {
    httpOnly: true,
    secure,
    sameSite: "lax" as const,
    path: "/",
    maxAge: 15 * 60,
  };
}

export function clearedPendingTermsCookieOptions() {
  return { ...pendingTermsCookieOptions(), maxAge: 0 };
}

export { PENDING_TERMS as PENDING_TERMS_COOKIE };
