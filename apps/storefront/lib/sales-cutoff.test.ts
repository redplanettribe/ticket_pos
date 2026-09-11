import assert from "node:assert/strict";
import test from "node:test";

import { formatSalesCutoff } from "./sales-cutoff.ts";

const CUTOFF = "2026-07-09T12:00:00Z";

test("the closing time is read on the Event's clock, not the viewer's", () => {
  // Noon UTC is 7:00 AM in Guayaquil and 2:00 PM in Madrid. The organizer set
  // one instant in the Event's timezone, and every buyer must be told that same
  // wall time however far away they are standing (ADR 0070).
  assert.match(formatSalesCutoff(CUTOFF, "America/Guayaquil") ?? "", /^Thu Jul 9.* 7:00 AM$/);
  assert.match(formatSalesCutoff(CUTOFF, "Europe/Madrid") ?? "", /^Thu Jul 9.* 2:00 PM$/);
});

test("the closing time is stated in the reader's language", () => {
  // The Storefront never shows the API's English: the date is formatted for the
  // Locale the page is being read in, as every other date on it is.
  assert.match(formatSalesCutoff(CUTOFF, "America/Guayaquil", "es-EC") ?? "", /jul/);
});

test("a Ticket Type with no Sales Cutoff has no closing line", () => {
  // The state every Ticket Type is in today. Null is "this never stops selling"
  // and gets no sentence at all, rather than a blank one.
  assert.equal(formatSalesCutoff(null, "America/Guayaquil"), null);
});

