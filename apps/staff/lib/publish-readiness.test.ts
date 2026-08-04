import assert from "node:assert/strict";
import test from "node:test";

import { getPublishMissingFields, PUBLISH_FIELD_LABELS } from "./events-api.ts";

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

test("every publish-readiness key the mirror can produce has a human label", () => {
  // The hint reads out these labels, so a key without one leaks a field name.
  for (const key of ["name", "slug", "starts_at", "timezone", "ticket_types", "registration_url"]) {
    assert.equal(typeof PUBLISH_FIELD_LABELS[key], "string", key);
  }
});
