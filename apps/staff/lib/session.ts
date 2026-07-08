const SESSION_COOKIE = "ticket_pos_session";

export function sessionCookieOptions() {
  const secure = process.env.NODE_ENV === "production";
  return {
    httpOnly: true,
    secure,
    sameSite: "lax" as const,
    path: "/",
    maxAge: 14 * 24 * 60 * 60,
  };
}

export { SESSION_COOKIE as SESSION_COOKIE_NAME };
