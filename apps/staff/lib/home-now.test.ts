import assert from "node:assert/strict";
import test from "node:test";

import { nextEventTimeZone, timeUntil } from "./home-now.ts";

const NOW = Date.parse("2026-07-07T12:00:00Z");

test("a start days away is counted in whole days, rounded down", () => {
  assert.deepEqual(timeUntil("2026-07-10T11:00:00Z", NOW), { value: 2, unit: "day" });
});

test("a start under a day away is counted in hours, never as one day", () => {
  assert.deepEqual(timeUntil("2026-07-08T11:59:00Z", NOW), { value: 23, unit: "hour" });
});

test("a start under an hour away is counted in minutes", () => {
  assert.deepEqual(timeUntil("2026-07-07T12:45:30Z", NOW), { value: 45, unit: "minute" });
});

test("a start already reached reads as zero, never negative", () => {
  assert.deepEqual(timeUntil("2026-07-07T10:00:00Z", NOW), { value: 0, unit: "minute" });
});

test("an Event's start is printed in its own zone, the platform's where it has none", () => {
  const event = { id: "e", name: "E", starts_at: "", tickets_sold: 0, ongoing: false };
  assert.equal(nextEventTimeZone({ ...event, timezone: "Europe/Madrid" }, "America/Guayaquil"), "Europe/Madrid");
  assert.equal(nextEventTimeZone({ ...event, timezone: "" }, "America/Guayaquil"), "America/Guayaquil");
});
