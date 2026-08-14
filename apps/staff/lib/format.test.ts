import assert from "node:assert/strict";
import test from "node:test";

import {
  PLATFORM_TIME_ZONE,
  formatCalendarDay,
  formatDate,
  formatDateTime,
  formatInstant,
  formatMoney,
  formatNumber,
  formatTime,
  staffIntlLocale,
} from "./format.ts";

/**
 * What is asserted here is THE RULE — a locale decides words and marks, and
 * decides nothing about time or money — and the rule is only checkable by
 * comparing the two languages against each other. So most of these tests format
 * the same value twice and say which parts moved and which did not.
 *
 * Exact rendered strings are avoided where ICU is free to change them between
 * Node releases (month abbreviations, the space before "PM"). What is asserted
 * instead is the invariant: the same instant, the same hour; the same amount, the
 * same currency; different language, different marks.
 */

// --- the tag ---------------------------------------------------------------

test("a Staff Locale maps to the regional tag its marks come from", () => {
  assert.equal(staffIntlLocale("en"), "en-US");
  assert.equal(staffIntlLocale("es"), "es-EC");
});

// --- numbers: the locale DOES decide the marks -----------------------------

test("a count is written with the reader's own group mark", () => {
  assert.equal(formatNumber(1500, "en"), "1,500");
  assert.equal(formatNumber(1500, "es"), "1.500");
});

test("the same count is the same quantity in both languages", () => {
  const digits = (value: string) => value.replace(/\D/g, "");
  assert.equal(digits(formatNumber(1234567, "en")), digits(formatNumber(1234567, "es")));
});

// --- money: the locale decides NOTHING about which currency ----------------

test("an amount stays in the currency it was given, whichever language reads it", () => {
  // Spanish does not turn dollars into euros, and does not drop the currency.
  for (const locale of ["en", "es"] as const) {
    assert.match(formatMoney(2500, "USD", locale), /\$|USD/);
    assert.match(formatMoney(2500, "EUR", locale), /€|EUR/);
  }
});

test("an Organization selling in one currency reads the same amount in both languages", () => {
  const english = formatMoney(123456, "USD", "en");
  const spanish = formatMoney(123456, "USD", "es");
  const digits = (value: string) => value.replace(/\D/g, "");
  // Same money, differently punctuated — never a different number.
  assert.equal(digits(english), digits(spanish));
  assert.notEqual(english, spanish);
});

test("cents are minor units, not dollars", () => {
  assert.match(formatMoney(2500, "USD", "en"), /25([.,]00)?/);
});

// --- time: the locale decides NOTHING about the zone -----------------------

// 2026-07-12T00:00:00Z is the 11th of July, 7pm, in Guayaquil (UTC-5).
const MIDNIGHT_UTC = "2026-07-12T00:00:00Z";

test("an instant is drawn in the zone it was given, not in the reader's", () => {
  const guayaquil = formatDateTime(MIDNIGHT_UTC, "America/Guayaquil", "en")!;
  const utc = formatDateTime(MIDNIGHT_UTC, "UTC", "en")!;
  assert.match(guayaquil, /11/);
  assert.match(utc, /12/);
});

test("the language never moves the hour", () => {
  const hourOf = (value: string) => value.replace(/\D/g, "");
  assert.equal(
    hourOf(formatTime(MIDNIGHT_UTC, "America/Guayaquil", "en")!),
    hourOf(formatTime(MIDNIGHT_UTC, "America/Guayaquil", "es")!),
  );
});

test("an Event's day is the Event's day in both languages", () => {
  const english = formatInstant(MIDNIGHT_UTC, "America/Guayaquil", "en", { day: "numeric" });
  const spanish = formatInstant(MIDNIGHT_UTC, "America/Guayaquil", "es", { day: "numeric" });
  assert.equal(english, "11");
  assert.equal(spanish, "11");
});

test("an explicit zone cannot be overridden by the options a surface asks for", () => {
  // The zone is applied last on purpose: a surface asking for a shape must not be
  // able to smuggle a second timeZone past the required argument.
  const drawn = formatInstant(MIDNIGHT_UTC, "UTC", "en", {
    dateStyle: "medium",
    timeZone: "America/Guayaquil",
  } as Intl.DateTimeFormatOptions);
  assert.match(drawn!, /12/);
});

test("dates and times still differ in their marks between the two languages", () => {
  assert.notEqual(
    formatDate(MIDNIGHT_UTC, "UTC", "en"),
    formatDate(MIDNIGHT_UTC, "UTC", "es"),
  );
});

// --- nothing to draw --------------------------------------------------------

test("a moment that has not happened draws nothing rather than a bad date", () => {
  assert.equal(formatDateTime(null, "UTC", "en"), null);
  assert.equal(formatDateTime(undefined, "UTC", "en"), null);
  assert.equal(formatDateTime("", "UTC", "en"), null);
  assert.equal(formatDateTime("not a date", "UTC", "en"), null);
});

test("a Date is accepted as readily as the API's ISO string", () => {
  assert.equal(
    formatDateTime(new Date(MIDNIGHT_UTC), "UTC", "en"),
    formatDateTime(MIDNIGHT_UTC, "UTC", "en"),
  );
});

// --- a calendar day is not a moment ----------------------------------------

test("a calendar day is the day it says, west of Greenwich too", () => {
  // The bug this function exists for: new Date("2026-03-01") is UTC midnight,
  // which is the 28th of February in Ecuador.
  assert.match(formatCalendarDay("2026-03-01", "en"), /\b1\b/);
  assert.match(formatCalendarDay("2026-03-01", "en"), /2026/);
  assert.ok(!formatCalendarDay("2026-03-01", "en").includes("28"));
});

test("a calendar day is written with the reader's marks", () => {
  assert.notEqual(formatCalendarDay("2026-03-01", "en"), formatCalendarDay("2026-03-01", "es"));
});

test("a value that is not a calendar day is handed back untouched", () => {
  assert.equal(formatCalendarDay("", "en"), "");
  assert.equal(formatCalendarDay("whenever", "en"), "whenever");
});

// --- the platform's own clock ----------------------------------------------

test("the platform clock is Ecuador's, and is a value rather than a default", () => {
  assert.equal(PLATFORM_TIME_ZONE, "America/Guayaquil");
  assert.equal(
    formatDateTime(MIDNIGHT_UTC, PLATFORM_TIME_ZONE, "en"),
    formatDateTime(MIDNIGHT_UTC, "America/Guayaquil", "en"),
  );
});
