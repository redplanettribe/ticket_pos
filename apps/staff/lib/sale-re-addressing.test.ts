import assert from "node:assert/strict";
import test from "node:test";

import type { OperatorSaleReAddressing } from "./operator-api.ts";
import type { AcceptedReAddressing } from "./sale-re-addressing.ts";
import {
  acceptedReAddressings,
  correctedEmailProblem,
  reAddressBody,
  reAddressingPanel,
  resendBody,
} from "./sale-re-addressing.ts";

const now = new Date("2026-07-07T12:00:00Z");
const event = {
  id: "e1",
  name: "Readdress Fest",
  slug: "readdress-fest",
  starts_at: "2026-07-10T12:00:00Z",
  timezone: "America/Guayaquil",
};
const active = { status: "active", channel: "online", event };
const pending: OperatorSaleReAddressing = {
  id: "r1",
  ticket_sale_id: "s1",
  confirmation_ref: "TP-ABC",
  status: "pending",
  previous_email: "ana.lopes@example.com",
  corrected_email: "ana.lopez@example.com",
  operator: "operator@example.com",
  note: null,
  requested_at: "2026-07-07T12:00:00Z",
  accepted_at: null,
  withdrawn_at: null,
};

test("the panel shows the form on an active online sale with nothing pending", () => {
  assert.deepEqual(reAddressingPanel(active, { pending: null, accepted: [] }, now), { kind: "form" });
  assert.deepEqual(reAddressingPanel(active, null, now), { kind: "form" });
});

test("the panel shows the form again once the pending record has been withdrawn", () => {
  // The lookup lists only what is pending and what was accepted; a withdrawn
  // record leaves `pending` null, and the form is offered again.
  assert.deepEqual(reAddressingPanel(active, { pending: null, accepted: [] }, now), { kind: "form" });
  // An accepted history does not stop a further re-addressing either.
  const accepted = { ...pending, status: "accepted", accepted_at: "2026-07-07T13:00:00Z" };
  assert.deepEqual(reAddressingPanel(active, { pending: null, accepted: [accepted] }, now), {
    kind: "form",
  });
});

test("the panel shows the pending card once a recording stands", () => {
  assert.deepEqual(reAddressingPanel(active, { pending, accepted: [] }, now), {
    kind: "pending",
    record: pending,
  });
});

test("the panel is hidden on a reversed, non-online, or started sale", () => {
  assert.deepEqual(reAddressingPanel({ ...active, status: "reversed" }, null, now), { kind: "hidden" });
  assert.deepEqual(reAddressingPanel({ ...active, channel: "import" }, null, now), { kind: "hidden" });
  assert.deepEqual(reAddressingPanel({ ...active, channel: "in_person" }, null, now), { kind: "hidden" });
  const started = { ...event, starts_at: "2026-07-07T11:59:59Z" };
  assert.deepEqual(reAddressingPanel({ ...active, event: started }, { pending, accepted: [] }, now), {
    kind: "hidden",
  });
  // The doors open at the instant itself, not a second later.
  const atNow = { ...event, starts_at: "2026-07-07T12:00:00Z" };
  assert.deepEqual(reAddressingPanel({ ...active, event: atNow }, null, now), { kind: "hidden" });
  // An Event with no start cannot have started.
  assert.deepEqual(reAddressingPanel({ ...active, event: { ...event, starts_at: null } }, null, now), {
    kind: "form",
  });
});

// Typed as the history card reads it: an accepted row keeps its address by
// database constraint, so `corrected_email` is a string here, never null.
const accepted: AcceptedReAddressing = {
  ...pending,
  id: "r0",
  status: "accepted",
  corrected_email: "ana.lopez@example.com",
  requested_at: "2026-07-06T12:00:00Z",
  accepted_at: "2026-07-06T12:30:00Z",
};

test("the panel is hidden on a reversed or started sale even while a record is pending", () => {
  // A reversal by any route, or the doors opening, leaves the pending record
  // expired: the API still returns null for it, but the page must not offer
  // the card even if a stale lookup carried one (#424).
  assert.deepEqual(reAddressingPanel({ ...active, status: "reversed" }, { pending, accepted: [] }, now), {
    kind: "hidden",
  });
  const started = { ...event, starts_at: "2026-07-07T12:00:00Z" };
  assert.deepEqual(reAddressingPanel({ ...active, event: started }, { pending, accepted: [] }, now), {
    kind: "hidden",
  });
});

test("the accepted history is listed whatever face the panel shows", () => {
  const block = { pending: null, accepted: [accepted] };
  // Hidden on a reversed sale, and the history still reads.
  assert.deepEqual(reAddressingPanel({ ...active, status: "reversed" }, block, now), { kind: "hidden" });
  assert.deepEqual(acceptedReAddressings(block), [accepted]);
  // Hidden on a started sale, likewise.
  const started = { ...event, starts_at: "2026-07-01T12:00:00Z" };
  assert.deepEqual(reAddressingPanel({ ...active, event: started }, block, now), { kind: "hidden" });
  assert.deepEqual(acceptedReAddressings(block), [accepted]);
  // Hidden on a non-online sale, likewise.
  assert.deepEqual(reAddressingPanel({ ...active, channel: "import" }, block, now), { kind: "hidden" });
  assert.deepEqual(acceptedReAddressings(block), [accepted]);
  // And on an active sale the form is offered again, above the history: a
  // sale may in principle be re-addressed twice.
  assert.deepEqual(reAddressingPanel(active, block, now), { kind: "form" });
  assert.deepEqual(acceptedReAddressings({ pending, accepted: [accepted] }), [accepted]);
  // The history never shows a purged address: every accepted row carries one.
  for (const record of acceptedReAddressings(block)) {
    assert.equal(typeof record.corrected_email, "string");
  }
  // Nothing accepted, or no block at all, is an empty list rather than a crash.
  assert.deepEqual(acceptedReAddressings({ pending: null, accepted: [] }), []);
  assert.deepEqual(acceptedReAddressings(null), []);
  assert.deepEqual(acceptedReAddressings(undefined), []);
});

test("correctedEmailProblem catches the empty field, a non-address, and the sale's own address", () => {
  assert.equal(correctedEmailProblem("   ", "ana.lopes@example.com"), "required");
  assert.equal(correctedEmailProblem("ana.lopez.example.com", "ana.lopes@example.com"), "invalid");
  assert.equal(correctedEmailProblem("@example.com", "ana.lopes@example.com"), "invalid");
  assert.equal(correctedEmailProblem("ana@", "ana.lopes@example.com"), "invalid");
  // The same address, however capitalised or padded: both normalise the same way.
  assert.equal(correctedEmailProblem("  ANA.Lopes@Example.com ", "ana.lopes@example.com"), "same");
  assert.equal(correctedEmailProblem("ana.lopez@example.com", "ana.lopes@example.com"), null);
});

test("reAddressBody trims the address and sends the note only when there is one", () => {
  assert.deepEqual(reAddressBody("  Ana.Lopez@Example.com ", "   "), { email: "Ana.Lopez@Example.com" });
  assert.deepEqual(reAddressBody("ana.lopez@example.com", "  buyer wrote in "), {
    email: "ana.lopez@example.com",
    note: "buyer wrote in",
  });
});

test("resendBody records the pending address and note again, and nothing once the address is purged", () => {
  assert.deepEqual(resendBody(pending), { email: "ana.lopez@example.com" });
  assert.deepEqual(resendBody({ ...pending, note: "buyer wrote in" }), {
    email: "ana.lopez@example.com",
    note: "buyer wrote in",
  });
  assert.equal(resendBody({ ...pending, corrected_email: null }), null);
});
