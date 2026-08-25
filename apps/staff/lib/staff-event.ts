import { cache } from "react";

import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import type { EventDetail } from "@/lib/events-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

/**
 * The Event a staff page is standing on, resolved once per request and shared
 * by everything under it that asks.
 *
 * The Event layout needs the whole payload (name, status, feature flags); the
 * Sales, Holder List and Affiliate Links pages beneath it need the timezone
 * their dates are drawn in, and Sales the Ticket Questions flag too. Each used
 * to read the session cookie and call the Event endpoint on its own — four
 * copies of the same tolerance for failure (#446). This is the one place that
 * fetch, and what counts as a failure, is decided.
 *
 * `cache()` is what makes the pages' reads free: a layout and its page render
 * within the same request, so the page's call resolves to the layout's answer
 * without a second round trip to the API.
 *
 * Failure — no session cookie, or an Event the API will not serve — is null,
 * not an error. What that means is each caller's decision: the layout answers
 * a not-found, the pages render anyway and let their dates fall back to the
 * platform timezone, which is a worse answer than the right one and a much
 * better one than no page.
 */
export const loadEvent = cache(async (eventId: string): Promise<EventDetail | null> => {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return null;
  }

  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return envelope.data;
  } catch {
    return null;
  }
});
