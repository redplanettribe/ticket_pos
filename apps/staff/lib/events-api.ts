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
  discoverable: boolean;
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
};

export type TicketType = {
  id: string;
  event_id: string;
  name: string;
  description: string | null;
  price_cents: number;
  currency: string;
  capacity: number;
  sold_count: number;
  sort_order: number;
  created_at: string;
  updated_at: string;
};

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

export function formatPriceCents(priceCents: number, currency: string): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency,
  }).format(priceCents / 100);
}

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

export const COMMON_TIMEZONES = [
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

export function formatEventStartDate(startsAt: string | null, timezone: string | null): string {
  if (!startsAt) {
    return "No date set";
  }
  const date = new Date(startsAt);
  const options: Intl.DateTimeFormatOptions = {
    dateStyle: "medium",
    timeStyle: "short",
    ...(timezone ? { timeZone: timezone } : {}),
  };
  return new Intl.DateTimeFormat(undefined, options).format(date);
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

export function getPublishMissingFields(
  name: string,
  slug: string,
  startsAtLocal: string,
  timezone: string,
  ticketTypeCount: number,
): string[] {
  const missing: string[] = [];
  if (!name.trim()) {
    missing.push("name");
  }
  if (!slug.trim()) {
    missing.push("slug");
  }
  if (!startsAtLocal) {
    missing.push("starts_at");
  }
  if (!timezone.trim()) {
    missing.push("timezone");
  }
  if (ticketTypeCount === 0) {
    missing.push("ticket_types");
  }
  return missing;
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
