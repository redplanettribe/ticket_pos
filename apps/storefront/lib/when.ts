// Date presets for the global explorer. Each maps to an optional RFC3339
// [from, to] window over an Event's start time, computed from "now".

export type WhenPreset = "all" | "weekend" | "week" | "month";

/**
 * The presets in the order the chip bar offers them — widest first, then the
 * three windows from nearest to furthest.
 *
 * Values only. A preset's label is copy, and copy is per-locale: it lives in the
 * catalog under `explorer.when.<value>` so this module stays a pure function of
 * dates that a unit test can run without a request or a translator.
 */
export const WHEN_PRESETS: readonly WhenPreset[] = ["all", "weekend", "week", "month"];

export function isWhenPreset(value: string | undefined): value is WhenPreset {
  return value === "all" || value === "weekend" || value === "week" || value === "month";
}

export function whenToRange(
  preset: WhenPreset,
  now: Date = new Date(),
): { from?: string; to?: string } {
  switch (preset) {
    case "week": {
      const to = new Date(now);
      to.setDate(to.getDate() + 7);
      return { from: now.toISOString(), to: to.toISOString() };
    }
    case "month": {
      const to = new Date(now);
      to.setMonth(to.getMonth() + 1);
      return { from: now.toISOString(), to: to.toISOString() };
    }
    case "weekend": {
      // The weekend in progress, or else the coming one: Saturday 00:00 through
      // Sunday 23:59:59 (local server time).
      const start = new Date(now);
      const day = start.getDay(); // 0 Sun … 6 Sat
      // Sunday is the back half of a weekend that has already begun, so its
      // Saturday is yesterday. Wrapping it forward instead — as (6 - day + 7) % 7
      // does — sends a Customer browsing on Sunday to *next* weekend and hides
      // everything happening today.
      const daysUntilSaturday = day === 0 ? -1 : 6 - day;
      start.setDate(start.getDate() + daysUntilSaturday);
      start.setHours(0, 0, 0, 0);
      const end = new Date(start);
      end.setDate(end.getDate() + 1);
      end.setHours(23, 59, 59, 0);
      // If we're already in the weekend, include from now.
      const from = start.getTime() < now.getTime() ? now : start;
      return { from: from.toISOString(), to: end.toISOString() };
    }
    case "all":
    default:
      return {};
  }
}
