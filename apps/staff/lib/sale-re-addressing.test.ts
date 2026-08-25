import assert from "node:assert/strict";
import test from "node:test";

import type { OperatorSaleReAddressing } from "./operator-api.ts";
import { correctedEmailProblem, reAddressBody, reAddressingPanel } from "./sale-re-addressing.ts";

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
