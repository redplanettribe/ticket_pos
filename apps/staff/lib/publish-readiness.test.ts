import assert from "node:assert/strict";
import test from "node:test";

import { readFileSync } from "node:fs";

import { getPublishMissingFields, isPublishFieldKey, PUBLISH_FIELD_KEYS } from "./events-api.ts";

const ready = {
  name: "Summer Fest",
  slug: "summer-fest",
  starts_at: "2026-08-01T20:00:00Z",
  timezone: "America/Guayaquil",
};

test("a ticketed Event is still missing ticket types until it has one", () => {
  // The regression guard: External Registration changed nothing for an Event
  // that sells tickets here.
  assert.deepEqual(getPublishMissingFields(ready, 0), ["ticket_types"]);
  assert.deepEqual(getPublishMissingFields(ready, 1), []);
  // An Event that says nothing about its mode reads as ticketed, the same
  // default the server applies to a row it does not recognise.
  assert.deepEqual(getPublishMissingFields({ ...ready, registration_mode: "tickets" }, 0), ["ticket_types"]);
});

test("an externally registered Event is missing its Registration Link, never ticket types", () => {
  // Naming ticket types here would send the organizer to add something the
  // Event cannot have: an external Event never sells any.
  for (const link of [null, undefined, "", "   "]) {
    assert.deepEqual(
      getPublishMissingFields({ ...ready, registration_mode: "external", registration_url: link }, 0),
      ["registration_url"],
      JSON.stringify(link),
    );
  }
});

test("an externally registered Event with a Registration Link and no ticket types is ready to publish", () => {
  assert.deepEqual(
    getPublishMissingFields(
      { ...ready, registration_mode: "external", registration_url: "https://lu.ma/my-meetup" },
      0,
    ),
    [],
  );
});

test("publish readiness still demands the Event's own fields in either mode", () => {
  const bare = { name: "", slug: "", starts_at: null, timezone: null };
  assert.deepEqual(getPublishMissingFields(bare, 0), ["name", "slug", "starts_at", "timezone", "ticket_types"]);
  assert.deepEqual(getPublishMissingFields({ ...bare, registration_mode: "external" }, 0), [
    "name",
    "slug",
    "starts_at",
    "timezone",
    "registration_url",
  ]);
});

test("every publish-readiness key the mirror can produce is one it names", () => {
  // The hint reads out these keys, so a key the mirror emits but does not
  // acknowledge would leak a raw field name into the sentence.
  for (const key of ["name", "slug", "starts_at", "timezone", "ticket_types", "registration_url"]) {
    assert.ok(isPublishFieldKey(key), key);
  }
  // A key the server invents later is not one of ours, and the hint shows it
  // raw rather than dropping the requirement.
  assert.equal(isPublishFieldKey("venue_name"), false);
});

test("every publish-readiness key has words in both languages", () => {
  // The labels moved to the catalogs (ADR 0041). A key added to the mirror
  // without copy is caught here rather than by a Spanish reader.
  const keyFor = (field: string) =>
    "publishField" +
    field
      .split("_")
      .map((part) => part[0].toUpperCase() + part.slice(1))
      .join("");

  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { event: Record<string, string> };
    for (const key of PUBLISH_FIELD_KEYS) {
      assert.ok(catalog.event[keyFor(key)], `${locale}.json is missing ${keyFor(key)}`);
    }
  }
});
