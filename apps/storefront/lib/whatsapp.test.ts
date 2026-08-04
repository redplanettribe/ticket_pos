import assert from "node:assert/strict";
import test from "node:test";

import { whatsappLink } from "./whatsapp.ts";

// The canonical form the API stores and serves (migration 049).
const NUMBER = "+593987654321";

test("the leading plus is stripped, because wa.me fails silently with it", () => {
  // The one step that has to be right. A link carrying the plus does not open a
  // conversation and reports nothing — there is no error for a caller to catch,
  // only a Customer who taps support and lands nowhere.
  assert.equal(whatsappLink(NUMBER), "https://wa.me/593987654321");
});

test("a number is reachable from a desktop as well as a phone", () => {
  // wa.me resolves to the app on a phone and to WhatsApp Web otherwise, so the
  // host is the whole of the cross-device story: no separate branch, no UA
  // sniffing.
  assert.ok(whatsappLink(NUMBER)?.startsWith("https://wa.me/"));
});

test("the prefill is appended URL-encoded", () => {
  assert.equal(
    whatsappLink(NUMBER, "Hi, a question about Rock Fest"),
    "https://wa.me/593987654321?text=Hi%2C%20a%20question%20about%20Rock%20Fest",
  );
});

test("an Event name with punctuation survives the prefill", () => {
  // Not hypothetical: ampersands, plus signs, hashes and accents are ordinary in
  // Event names, and every one of them means something else in a query string.
  const link = whatsappLink(NUMBER, "Hi, a question about Sam & Dave +1 #2 — Fiesta Ñandú");
  assert.equal(
    link,
    "https://wa.me/593987654321?text=Hi%2C%20a%20question%20about%20Sam%20%26%20Dave%20%2B1%20%232%20%E2%80%94%20Fiesta%20%C3%91and%C3%BA",
  );
  // The round trip is the real claim: whatever WhatsApp decodes must be exactly
  // what the Organization was meant to read.
  const text = new URL(link!).searchParams.get("text");
  assert.equal(text, "Hi, a question about Sam & Dave +1 #2 — Fiesta Ñandú");
});

test("no prefill means no query string at all", () => {
  assert.equal(whatsappLink(NUMBER), "https://wa.me/593987654321");
  assert.equal(whatsappLink(NUMBER, ""), "https://wa.me/593987654321");
});

test("a stray space is survived, but nothing here normalises a number", () => {
  // The guard: whitespace that should never have arrived does not break the link.
  assert.equal(whatsappLink(" +593987654321 "), "https://wa.me/593987654321");

  // The limit of that guard, stated so nobody mistakes it for normalisation.
  // A number as a human writes it keeps its Ecuadorian trunk zero here —
  // 5930987654321, a different and unreachable number — because turning typed
  // input into a canonical one is a rule about national numbering plans that
  // lives on the server (platform.ValidatePhone), and duplicating it here is
  // exactly the drift that file warns against. The API only ever serves
  // canonical numbers, so this input cannot occur; the assertion exists to keep
  // the boundary honest rather than to bless the output.
  assert.notEqual(whatsappLink("+593 (0)98-765.4321"), "https://wa.me/593987654321");
});

test("a number with no digits yields no link", () => {
  // So a caller renders nothing rather than a link to WhatsApp's homepage.
  assert.equal(whatsappLink(""), null);
  assert.equal(whatsappLink("+"), null);
});
