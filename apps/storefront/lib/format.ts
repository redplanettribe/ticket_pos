// Locale and formatting rules follow docs/design/foundation.md:
// USD-style "$25" in the catalog, "$25.00" only on summed totals; Storefront
// dates render in the Event's timezone as "Saturday, July 12, 2026 · 7:00 PM".

export function formatPrice(cents: number, currency: string): string {
  const hasFraction = cents % 100 !== 0;
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
    minimumFractionDigits: hasFraction ? 2 : 0,
    maximumFractionDigits: 2,
  }).format(cents / 100);
}

export function formatPriceFrom(cents: number | null, currency: string): string | null {
  if (cents === null) return null;
  if (cents === 0) return "Free";
  return `From ${formatPrice(cents, currency)}`;
}

function timeZoneOrUndefined(timezone: string | null): string | undefined {
  return timezone ?? undefined;
}

// Long form for event pages: "Saturday, July 12, 2026 · 7:00 PM".
export function formatEventDateTime(startsAt: string | null, timezone: string | null): string | null {
  if (!startsAt) return null;
  const date = new Date(startsAt);
  if (Number.isNaN(date.getTime())) return null;

  const day = new Intl.DateTimeFormat("en-US", {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone: timeZoneOrUndefined(timezone),
  }).format(date);

  const time = new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timeZoneOrUndefined(timezone),
  }).format(date);

  return `${day} · ${time}`;
}

// Compact form for cards: "Sat, Jul 12 · 7:00 PM".
export function formatEventDateShort(startsAt: string | null, timezone: string | null): string | null {
  if (!startsAt) return null;
  const date = new Date(startsAt);
  if (Number.isNaN(date.getTime())) return null;

  return new Intl.DateTimeFormat("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZone: timeZoneOrUndefined(timezone),
  })
    .format(date)
    .replace(",", "")
    .replace(" at ", " · ");
}
