import type { FeeHandling } from "./fees";
import type { Promotion } from "./promotions";
import type { RegistrationMode } from "./registration";

export type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: Record<string, unknown> } | null;
};

export class ApiError extends Error {
  code?: string;
  details?: Record<string, unknown>;

  constructor(message: string, code?: string, details?: Record<string, unknown>) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.details = details;
  }
}

export type EventListItem = {
  id: string;
  name: string;
  slug: string;
  status: string;
  starts_at: string | null;
  /** The Event's end instant, or null when it has none — which is most of
   * them. It rides on the list row so the Events tabs can decide whether an
   * Event is over without fetching each Event in full, falling back to
   * starts_at where there is no end (#611). */
  ends_at: string | null;
  timezone: string | null;
  discoverable: boolean;
  created_at: string;
};

export type EventDetail = {
  id: string;
  name: string;
  slug: string;
  status: string;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string | null;
  venue_name: string | null;
  venue_address: string | null;
  description: string | null;
  cover_image_key: string | null;
  cover_image_url: string | null;
  /** The Event's optional Cover Video: the stored key and the URL derived from
   * it at read time. Plays in the Storefront hero over the cover image. */
  cover_video_key: string | null;
  cover_video_url: string | null;
  discoverable: boolean;
  /** How this Event handles the Platform Fee and its Fee IVA (ADR 0014). */
  fee_handling: FeeHandling;
  /** The fee schedule in force, in basis points — the derived-line inputs. */
  fee_basis_points: number;
  fee_iva_basis_points: number;
  /** How this Event takes sign-ups: tickets here, or a Registration Link
   * elsewhere. Never both (ADR 0028). */
  registration_mode: RegistrationMode;
  /** The Registration Link, null while an external Event's registration page is
   * still being built. */
  registration_url: string | null;
  /** Hand-offs to the Registration Link. Clicks — never registrations, never
   * people: the platform loses sight of the buyer at the link. */
  registration_click_count: number;
  /** The platform's Ticket Question feature flag, not a property of this Event
   * — it rides here for the same reason the fee rates do, because the Ticket
   * Type editor is composed from this payload (#309, ADR 0045). False is the
   * shipped state, and with it false no surface offers Ticket Questions. */
  ticket_questions_enabled: boolean;
  /** The platform's Ticket Assignment feature flag, riding here for the same
   * reason: the Holder List entry is offered while EITHER this or the questions
   * flag is on (#333), and the staff app reads both off this payload rather
   * than off an env var of its own. False is the shipped state. */
  ticket_assignment_enabled: boolean;
  created_at: string;
};

export type CoverUploadURL = {
  upload_url: string;
  object_key: string;
  public_url: string;
};

export type EventPatchBody = {
  name: string;
  slug: string;
  starts_at: string | null;
  ends_at: string | null;
  timezone: string;
  venue_name: string;
  venue_address: string;
  description: string;
  fee_handling: FeeHandling;
  /**
   * The External Registration pair, both optional because the API's rule is
   * that an absent field leaves the stored value alone. The side-saves that
   * borrow this body — the cover image and the cover video — deliberately omit
   * them, so a half-typed Registration Link can never block an upload from
   * saving. "" clears the link.
   */
  registration_mode?: RegistrationMode;
  registration_url?: string;
};

export type TicketType = {
  id: string;
  event_id: string;
  name: string;
  description: string | null;
  /** The List Price. It stays put whether or not a Promotion is live (ADR 0021). */
  price_cents: number;
  currency: string;
  capacity: number;
  sold_count: number;
  sort_order: number;
  /**
   * The Purchase Limit: the most of this Ticket Type one Customer may hold at
   * once (CONTEXT.md, ADR 0025). null means there is no Purchase Limit, which
   * is the case on most Ticket Types. It governs future checkouts only, so a
   * Customer may legitimately hold more than this.
   */
  max_per_customer: number | null;
  /**
   * The Sales Cutoff: the instant the Storefront stops selling this Ticket Type
   * (CONTEXT.md, ADR 0070). null means it never stops, which is the case on
   * most Ticket Types.
   */
  sales_cutoff_at: string | null;
  /** The one Promotion slot, or null when it is empty. */
  promotion: Promotion | null;
  created_at: string;
  updated_at: string;
};

/**
 * The body the Ticket Type update endpoint takes: every field a Ticket Type
 * carries that an organizer can change, and nothing else.
 *
 * The endpoint is a FULL RESTATEMENT and not a partial patch — an omitted key
 * clears the field. So every caller has to name every field, and there are
 * three of them: the edit dialog, and the two halves of a reorder. Writing that
 * list out three times is how the Purchase Limit and then the Sales Cutoff each
 * came within one careless edit of being wiped by somebody nudging a row up or
 * down, neither being a field an organizer would think to re-check afterwards.
 * So the list is written once, here, and the three call sites say only what they
 * are changing.
 */
export type TicketTypeRestatement = {
  name: string;
  description: string | null;
  price_cents: number;
  capacity: number;
  max_per_customer: number | null;
  sales_cutoff_at: string | null;
  sort_order: number;
};

/**
 * A Ticket Type restated as it stands, with `changes` applied over it.
 *
 * `changes` is the whole of what a call site means to do. Everything it does
 * not mention is echoed back unchanged, which is the only way an omission here
 * can be a deliberate one.
 */
export function ticketTypeRestatement(
  ticketType: TicketType,
  changes: Partial<TicketTypeRestatement>,
): TicketTypeRestatement {
  return {
    name: ticketType.name,
    description: ticketType.description,
    price_cents: ticketType.price_cents,
    capacity: ticketType.capacity,
    max_per_customer: ticketType.max_per_customer,
    sales_cutoff_at: ticketType.sales_cutoff_at,
    sort_order: ticketType.sort_order,
    ...changes,
  };
}

export type Tag = {
  name: string;
  curated: boolean;
};

export const TAG_NAME_MAX_LENGTH = 30;
const TAG_NAME_PATTERN = /^[\p{L}\p{N} -]{1,30}$/u;

export function isValidTagName(value: string): boolean {
  return TAG_NAME_PATTERN.test(value.trim());
}

export function tagCanonicalKey(name: string): string {
  return name.trim().replace(/\s+/g, " ").toLowerCase();
}

export async function searchTags(query: string): Promise<Tag[]> {
  return fetchEventsJSON<Tag[]>(`/api/tags?q=${encodeURIComponent(query)}`);
}

export async function listPopularTags(): Promise<Tag[]> {
  return fetchEventsJSON<Tag[]>(`/api/tags/popular`);
}

export async function getEventTags(eventId: string): Promise<Tag[]> {
  return fetchEventsJSON<Tag[]>(`/api/events/${eventId}/tags`);
}

export async function setEventTags(eventId: string, tags: string[]): Promise<Tag[]> {
  return fetchEventsJSON<Tag[]>(`/api/events/${eventId}/tags`, {
    method: "PUT",
    body: JSON.stringify({ tags }),
  });
}

export const SUPPORTED_CURRENCIES = [
  "USD",
  "EUR",
  "GBP",
  "CAD",
  "AUD",
  "JPY",
  "CHF",
  "MXN",
  "BRL",
  "NZD",
  "SEK",
  "NOK",
  "DKK",
] as const;

// AN AMOUNT IS DRAWN BY `formatMoney` in lib/format.ts, which demands the Staff
// Locale and the Organization's currency and has no default for either. The
// bare-`Intl` wrapper that used to live here followed the BROWSER's locale,
// which is the bug ADR 0041 exists to fix; it survived only for the surfaces
// still to be migrated, and #292 migrated the last of them.

export function parsePriceToCents(value: string): number | null {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number.parseFloat(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return null;
  }
  return Math.round(parsed * 100);
}

// Fallback for runtimes without Intl.supportedValuesOf (older browsers/Node).
const FALLBACK_TIMEZONES = [
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Phoenix",
  "Pacific/Honolulu",
  "UTC",
  "Europe/London",
  "Europe/Paris",
];

// The full IANA timezone list, straight from the JS engine's tz database, so
// organizers can pick any zone rather than a curated handful. The backend
// validates against Go's embedded IANA database, so both stay in sync.
export const AVAILABLE_TIMEZONES: string[] = (() => {
  const withValues = Intl as typeof Intl & {
    supportedValuesOf?: (key: string) => string[];
  };
  if (typeof withValues.supportedValuesOf === "function") {
    try {
      const zones = withValues.supportedValuesOf("timeZone");
      if (zones.length > 0) {
        return zones.includes("UTC") ? zones : ["UTC", ...zones];
      }
    } catch {
      // fall through to the static list
    }
  }
  return FALLBACK_TIMEZONES;
})();

export type TimezoneOption = {
  value: string;
  label: string;
  hint: string;
  offsetMinutes: number;
};

/** Current UTC offset of a zone, in minutes (DST-aware for "now"). */
function timezoneOffsetMinutes(timeZone: string, date: Date): number {
  // Format the same instant as UTC and as the target zone, then diff. This is
  // the standard offset trick that avoids parsing locale-specific strings.
  const dtf = new Intl.DateTimeFormat("en-US", {
    timeZone,
    hour12: false,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  const parts = Object.fromEntries(dtf.formatToParts(date).map((p) => [p.type, p.value]));
  const asUTC = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    // "24" can appear at midnight in some engines; normalise to 0.
    Number(parts.hour) % 24,
    Number(parts.minute),
    Number(parts.second),
  );
  return Math.round((asUTC - date.getTime()) / 60000);
}

function formatOffset(offsetMinutes: number): string {
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const abs = Math.abs(offsetMinutes);
  const hours = String(Math.floor(abs / 60)).padStart(2, "0");
  const minutes = String(abs % 60).padStart(2, "0");
  return `GMT${sign}${hours}:${minutes}`;
}

/**
 * Build display-ready timezone options: a readable label, a GMT-offset hint,
 * sorted west-to-east then alphabetically. Computed once per date at call time.
 */
export function getTimezoneOptions(date: Date = new Date()): TimezoneOption[] {
  return AVAILABLE_TIMEZONES.map((value) => {
    const offsetMinutes = timezoneOffsetMinutes(value, date);
    return {
      value,
      label: value.replace(/_/g, " "),
      hint: formatOffset(offsetMinutes),
      offsetMinutes,
    };
  }).sort((a, b) => a.offsetMinutes - b.offsetMinutes || a.value.localeCompare(b.value));
}

export async function fetchEventsJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new ApiError(envelope.error?.message ?? "Request failed", envelope.error?.code, envelope.error?.details);
  }
  if (envelope.data === null) {
    throw new Error("Empty response");
  }
  return envelope.data;
}

export async function setEventDiscoverable(eventId: string, discoverable: boolean): Promise<EventDetail> {
  return fetchEventsJSON<EventDetail>(`/api/events/${eventId}/discoverable`, {
    method: "PUT",
    body: JSON.stringify({ discoverable }),
  });
}

export function slugify(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// AN EVENT'S START is `formatDateTime` from lib/format.ts, drawn in the Event's
// own timezone, with `events.noDateSet` said by the catalog when there is none.
// The wrapper that used to live here was wrong twice over — a bare `Intl`
// follows the BROWSER's locale, and "No date set" is an English sentence in a
// module that has no business holding one — and it survived only for the
// surfaces still to be migrated. #292 migrated the last of them.

/**
 * The `events` catalog key an Event's status is written with, or null for a
 * status this app has never heard of.
 *
 * The status itself is the API's token — "draft", "published", "cancelled" — and
 * was rendered raw under a `capitalize` class, which is English by accident and
 * a lie in Spanish. Null rather than a fallback sentence: the caller shows the
 * raw token, so a status the backend adds later reads as something rather than
 * as a blank badge, exactly the way an unknown error code falls back to the
 * API's own words (ADR 0023).
 */
export function eventStatusKey(
  status: string,
): "statusDraft" | "statusPublished" | "statusCancelled" | null {
  switch (status) {
    case "draft":
      return "statusDraft";
    case "published":
      return "statusPublished";
    case "cancelled":
      return "statusCancelled";
    default:
      return null;
  }
}

export function statusBadgeVariant(status: string): "warning" | "success" | "destructive" | "secondary" {
  switch (status) {
    case "published":
      return "success";
    case "cancelled":
      return "destructive";
    case "draft":
      return "warning";
    default:
      return "secondary";
  }
}

export function missingFieldsFromDetails(details: Record<string, unknown> | undefined): string[] | null {
  const missing = details?.missing_fields;
  return Array.isArray(missing) ? missing.filter((field): field is string => typeof field === "string") : null;
}

/** The persisted Event fields publish-readiness depends on. */
export type PublishReadinessEvent = {
  name: string;
  slug: string;
  starts_at: string | null;
  timezone: string | null;
  /**
   * The registration pair. Optional so callers that predate External
   * Registration keep the ticketed reading, which is the same default the
   * server applies to a row it does not recognise.
   */
  registration_mode?: RegistrationMode;
  registration_url?: string | null;
};

/**
 * Pure publish-readiness check over SAVED server state. Returns the list of
 * missing requirement keys (empty when the persisted Event may be published).
 * Kept pure and dependency-free so it is directly unit-testable and can drive
 * the header bar's Publish button without any form state.
 *
 * A mirror of the server's publish gate, and wrong when it drifts from it: the
 * server is what decides, and this exists only so the button and its hint say
 * the same thing before the request is made.
 */
export function getPublishMissingFields(event: PublishReadinessEvent, ticketTypeCount: number): string[] {
  const missing: string[] = [];
  if (!event.name.trim()) {
    missing.push("name");
  }
  if (!event.slug.trim()) {
    missing.push("slug");
  }
  if (!event.starts_at) {
    missing.push("starts_at");
  }
  if (!event.timezone || !event.timezone.trim()) {
    missing.push("timezone");
  }
  // The way in is the mode's: an externally registered Event needs its
  // Registration Link and never a Ticket Type, so naming ticket types here would
  // point the organizer at something the Event does not have and cannot use.
  if (event.registration_mode === "external") {
    if (!event.registration_url || !event.registration_url.trim()) {
      missing.push("registration_url");
    }
  } else if (ticketTypeCount === 0) {
    missing.push("ticket_types");
  }
  return missing;
}

/**
 * Every requirement key the mirror above can produce, and the server with it.
 *
 * Keys, not labels. This was a map of English words — "at least one ticket
 * type" — read straight into the publish hint, which put copy in a pure module
 * and left a Spanish-reading organizer with an English list of what to fix. The
 * words are `event.publishFieldTicketTypes` and its siblings in the catalogs now
 * (ADR 0041), and the header bar looks each key up there.
 *
 * The list stays here because it is a fact about this mirror rather than about
 * the copy: the server may name a key this app has never heard of — the two can
 * drift, which is the whole reason the mirror is documented as advisory — and the
 * hint shows such a key raw rather than dropping a requirement silently.
 */
export const PUBLISH_FIELD_KEYS = [
  "name",
  "slug",
  "starts_at",
  "timezone",
  "ticket_types",
  "registration_url",
] as const;

export type PublishFieldKey = (typeof PUBLISH_FIELD_KEYS)[number];

/** Whether a missing-field key is one this app has words for. */
export function isPublishFieldKey(key: string): key is PublishFieldKey {
  return (PUBLISH_FIELD_KEYS as readonly string[]).includes(key);
}

function getTimeZoneOffsetMs(date: Date, timeZone: string): number {
  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  const parts = Object.fromEntries(
    formatter.formatToParts(date).filter((part) => part.type !== "literal").map((part) => [part.type, part.value]),
  );
  const asUTC = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    Number(parts.hour),
    Number(parts.minute),
    Number(parts.second),
  );
  return asUTC - date.getTime();
}

export function isoToDateTimeLocal(iso: string | null, timeZone: string): string {
  if (!iso) {
    return "";
  }
  const date = new Date(iso);
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  const parts = Object.fromEntries(formatter.formatToParts(date).map((part) => [part.type, part.value]));
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`;
}

export function dateTimeLocalToISO(localValue: string, timeZone: string): string | null {
  if (!localValue) {
    return null;
  }
  const [datePart, timePart] = localValue.split("T");
  if (!datePart || !timePart) {
    return null;
  }
  const [year, month, day] = datePart.split("-").map(Number);
  const [hour, minute] = timePart.split(":").map(Number);
  const utcGuess = Date.UTC(year, month - 1, day, hour, minute);
  const offsetMs = getTimeZoneOffsetMs(new Date(utcGuess), timeZone);
  return new Date(utcGuess - offsetMs).toISOString();
}
