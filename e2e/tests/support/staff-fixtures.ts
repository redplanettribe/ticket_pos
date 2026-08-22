import type { APIRequestContext } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

import { readPasscode } from "./passcode";

/**
 * Provisioning what a journey needs through the staff API, as an Organizer
 * would — against the dev stack's API, whose port is fixed by docker-compose.yml.
 *
 * THE FIXTURE IS OWNED BY THIS SUITE, NOT BORROWED FROM THE SEED. The dev-seed
 * Event (migrations 002, 008) is absent from a database restored from
 * production (`make prod-to-local`), and a spec keyed on it skips or fails
 * there for a reason that has nothing to do with the feature. So the Organizer
 * below is an `e2e-` address, the Organization is an `e2e-` slug, and both are
 * created on the first run and found on every run after. Nothing here reads or
 * touches rows this suite did not write.
 */

export const API_URL = process.env.E2E_API_URL ?? "http://localhost:64080";

/** The Organizer of the fixture Organization. A staff address, not a Customer. */
export const ORGANIZER_EMAIL = "e2e-organizer@example.com";

const ORG = { name: "E2E Assignment Venue", slug: "e2e-assignment-venue" };
const EVENT = { name: "E2E Assignment Night", slug: "e2e-assignment-night" };
export const FIXTURE_TICKET_TYPE = "General Admission";
/** Far enough ahead that Ticket Assignment's "before the Event starts" window never closes. */
const STARTS_AT = "2030-06-01T20:00:00Z";

type Envelope<T> = { data: T; error: { code: string; message: string } | null };

async function call<T>(
  request: APIRequestContext,
  method: "get" | "post" | "patch",
  path: string,
  token: string | null,
  data?: unknown,
): Promise<Envelope<T>> {
  const response = await request[method](`${API_URL}${path}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    data,
  });
  const envelope = (await response.json()) as Envelope<T>;
  if (envelope.error) {
    throw new Error(`${method.toUpperCase()} ${path}: ${envelope.error.code} ${envelope.error.message}`);
  }
  return envelope;
}

/**
 * Where the Organizer's session is kept between runs — its own gitignored
 * directory, because Playwright empties test-results/ on every start. A passcode is rationed — three per address per
 * quarter-hour (otp.maxPerEmail) — and a journey that spent one on every rerun
 * would lock its own Organizer out on the fourth. A staff session outlives a
 * run, so it is asked for once and reused until the API stops honouring it.
 */
const SESSION_FILE = path.join(process.cwd(), ".e2e-state", "organizer-session");

function rememberedSession(): string | null {
  try {
    return readFileSync(SESSION_FILE, "utf8").trim() || null;
  } catch {
    return null;
  }
}

function rememberSession(token: string) {
  mkdirSync(path.dirname(SESSION_FILE), { recursive: true });
  writeFileSync(SESSION_FILE, token);
}

/** Whether the API still honours a remembered session for THIS fixture's Organization. */
async function sessionIsLive(request: APIRequestContext, token: string): Promise<boolean> {
  const response = await request.get(`${API_URL}/api/v1/staff/me`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok()) return false;
  const me = (await response.json()) as { data: { organization_slug?: string } | null };
  return me.data?.organization_slug === ORG.slug;
}

/** Why no session could be had, worded for a `test.skip` message. */
export type NoSession = { skipped: string };

type Session = {
  session_id: string;
  session: { memberships: { member_id: string; organization_slug: string }[] };
};

/**
 * A staff session for the Organizer, scoped to the fixture Organization — or
 * the reason there is none, which is a reason to skip rather than fail: the
 * passcode cannot be read, which is the "not the dev stack" case every
 * signed-in journey already skips on, or the Organizer's fixed address has
 * asked for its three passcodes this quarter-hour (otp.maxPerEmail), which a
 * fourth rerun in a row does.
 *
 * The same two-step the Staff app performs (request, then verify with the code
 * from the log), then the Organization is chosen: created on the first run,
 * selected from the memberships after that.
 */
export async function organizerSession(
  request: APIRequestContext,
): Promise<{ token: string } | NoSession> {
  const remembered = rememberedSession();
  if (remembered !== null && (await sessionIsLive(request, remembered))) {
    return { token: remembered };
  }

  try {
    await call(request, "post", "/api/v1/auth/otp/request", null, { email: ORGANIZER_EMAIL });
  } catch (error) {
    if (error instanceof Error && error.message.includes("OTP_RATE_LIMITED")) {
      return {
        skipped: `${ORGANIZER_EMAIL} is rate-limited on passcodes — three runs per 15 minutes; wait and rerun`,
      };
    }
    throw error;
  }
  const code = readPasscode(ORGANIZER_EMAIL);
  if (code === null) {
    return { skipped: "no passcode in the API log — this journey needs the dev stack (`make dev`)" };
  }

  const verified = await call<Session>(request, "post", "/api/v1/auth/otp/verify", null, {
    email: ORGANIZER_EMAIL,
    code,
  });
  const token = verified.data.session_id;

  const membership = verified.data.session.memberships.find(
    (m) => m.organization_slug === ORG.slug,
  );
  if (membership) {
    await call(request, "post", "/api/v1/staff/session/organization", token, {
      member_id: membership.member_id,
    });
  } else {
    await call(request, "post", "/api/v1/staff/organizations", token, ORG);
  }
  rememberSession(token);
  return { token };
}

export type FixtureEvent = {
  eventId: string;
  ticketTypeId: string;
  /** The published Event's Storefront path, in the given Locale. */
  path: (locale: string) => string;
  name: string;
};

/**
 * The fixture Event: published, one General Admission Ticket Type with one
 * required short-text Ticket Question. Idempotent — every step looks before it
 * creates, so reruns pile up neither Events nor Questions.
 */
export async function ensureFixtureEvent(
  request: APIRequestContext,
  token: string,
  questionLabel: string,
): Promise<FixtureEvent> {
  type Event = { id: string; slug: string; status: string };
  const events = await call<Event[]>(request, "get", "/api/v1/staff/events", token);
  let event = events.data.find((e) => e.slug === EVENT.slug);
  if (!event) {
    event = (await call<Event>(request, "post", "/api/v1/staff/events", token, EVENT)).data;
    await call(request, "patch", `/api/v1/staff/events/${event.id}`, token, {
      ...EVENT,
      starts_at: STARTS_AT,
      timezone: "America/Guayaquil",
      venue_name: "The Hall",
    });
  }

  type TicketType = { id: string; name: string };
  const ticketTypes = await call<TicketType[]>(
    request,
    "get",
    `/api/v1/staff/events/${event.id}/ticket-types`,
    token,
  );
  let ticketType = ticketTypes.data.find((tt) => tt.name === FIXTURE_TICKET_TYPE);
  if (!ticketType) {
    ticketType = (
      await call<TicketType>(request, "post", `/api/v1/staff/events/${event.id}/ticket-types`, token, {
        name: FIXTURE_TICKET_TYPE,
        price_cents: 3500,
        capacity: 200,
      })
    ).data;
  }

  const questionsPath = `/api/v1/staff/events/${event.id}/ticket-types/${ticketType.id}/questions`;
  type Question = { label: string; retired: boolean };
  const questions = await call<Question[]>(request, "get", questionsPath, token);
  if (!questions.data.some((q) => q.label === questionLabel && !q.retired)) {
    await call(request, "post", questionsPath, token, {
      kind: "short_text",
      label: questionLabel,
      required: true,
    });
  }

  if (event.status !== "published") {
    await call(request, "post", `/api/v1/staff/events/${event.id}/publish`, token);
  }

  return {
    eventId: event.id,
    ticketTypeId: ticketType.id,
    path: (locale) => `/${locale}/${ORG.slug}/events/${EVENT.slug}`,
    name: EVENT.name,
  };
}

export type HolderListEntry = {
  assignment_state: "unassigned" | "assigned" | "accepted";
  holder_email: string;
  confirmation_ref: string;
  outstanding: string[] | null;
};

/** The Event's Holder List, as the Organizer reads it. */
export async function holderList(
  request: APIRequestContext,
  token: string,
  eventId: string,
): Promise<HolderListEntry[]> {
  const page = await call<{ data: HolderListEntry[] }>(
    request,
    "get",
    `/api/v1/staff/events/${eventId}/outstanding-answers?page_size=50`,
    token,
  );
  return page.data.data;
}
