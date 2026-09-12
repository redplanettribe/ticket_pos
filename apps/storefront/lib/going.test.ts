import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { goingLine } from "./going.ts";
import { LOCALES } from "./locale.ts";

// The "going" line (ADR 0072): the platform's Tickets Sold figure, rendered to
// a Customer as people going. The backend floors it at 5 and sends null
// beneath, so the Storefront's whole decision is one branch — a number is a
// line, null is nothing — and these tests hold it to exactly that.

test("a Tickets Sold figure becomes a going line carrying that count", () => {
  assert.deepEqual(goingLine({ tickets_sold: 40 }), { count: 40 });
  assert.deepEqual(goingLine({ tickets_sold: 5 }), { count: 5 });
});

// Null is the API withholding the figure — beneath the floor, or an Event
// with External Registration that sells nothing here. It is never a zero and
// never a line: "0 going" on a page that just opened reads as a failure, and
// "1 going" is a person who can read it.
test("a withheld figure is no line at all, never zero", () => {
  assert.equal(goingLine({ tickets_sold: null }), null);
});

// The floor is the backend's (ADR 0072: "one branch and no arithmetic"). A
// count that somehow arrives beneath it is stated as sent, because a second
// floor here would be a second place the two surfaces could disagree.
test("the helper applies no floor of its own", () => {
  assert.deepEqual(goingLine({ tickets_sold: 1 }), { count: 1 });
});

type Catalog = { [key: string]: string | Catalog };

function load(locale: string): Catalog {
  return JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  ) as Catalog;
}

// The helper returns data and the catalog supplies the words, so the line is
// only whole when every Locale can say it. No singular form is asserted
// because none exists: the floor guarantees the count is never below 5.
test("every catalog carries the going line and interpolates the count", () => {
  for (const locale of LOCALES) {
    const event = load(locale).event;
    assert.ok(event && typeof event === "object", `${locale}.json has no event namespace`);
    const message = (event as Catalog).going;
    assert.equal(typeof message, "string", `${locale}.json is missing event.going`);
    assert.match(message as string, /\{count\}/, `${locale}.json event.going does not interpolate {count}`);
  }
});
