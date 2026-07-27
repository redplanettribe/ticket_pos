import assert from "node:assert/strict";
import test from "node:test";

import {
  COUNTRIES,
  ECUADOR_DIALLING_CODE,
  PHONE_ECUADOR_MESSAGE,
  PHONE_GENERIC_MESSAGE,
  normalizePhone,
  splitPhone,
  validatePhone,
} from "./phone.ts";

// The mirror validator (#105). Its job is instant feedback, and its one hard
// requirement is agreeing with backend/internal/platform/phone.go — a number
// this file rejects but the API accepts is a buyer locked out of a sale the
// platform would have taken. The two case tables below are the same cases as
// backend/internal/platform/phone_test.go, name for name, for exactly that
// reason; when one gains a case the other must too.

// The strict tier. Ecuador is the home market and the default selection, and
// PayPhone's form wants a cardholder's mobile — so a landline is a rejection
// rather than a pass-through.
const ECUADOR_CASES: [name: string, input: string, want: string | null][] = [
  ["canonical mobile", "+593987654321", "+593987654321"],
  ["another canonical mobile", "+593991234567", "+593991234567"],
  ["lowest nine-leading mobile", "+593900000000", "+593900000000"],
  ["domestic trunk zero dropped", "+5930987654321", "+593987654321"],
  ["spaces as the buyer typed them", "+593 98 765 4321", "+593987654321"],
  ["dashes", "+593-98-765-4321", "+593987654321"],
  ["parenthesised trunk zero", "+593 (0)98 765 4321", "+593987654321"],
  ["dots", "+593.98.765.4321", "+593987654321"],
  ["surrounding whitespace", "  +593987654321  ", "+593987654321"],
  ["tab and newline", "\t+593987654321\n", "+593987654321"],

  ["quito landline", "+59322345678", null],
  ["quito landline with trunk zero", "+593022345678", null],
  ["guayaquil landline", "+59342345678", null],
  ["national part leading 8", "+593887654321", null],
  ["eight-digit national part", "+59398765432", null],
  ["ten-digit national part", "+5939876543210", null],
  ["dialling code alone", "+593", null],
  ["two leading zeros, only one is a trunk prefix", "+59300987654321", null],
  ["letters among the digits", "+59398765432a", null],
];

// The permissive tier. Its job is to prove the rule stays out of the way: no
// phone library is used, so anything that could plausibly be a foreign number
// must survive, and only the E.164 bounds bite.
const GENERIC_CASES: [name: string, input: string, want: string | null][] = [
  ["us number", "+12025550123", "+12025550123"],
  ["us number as typed", "+1 (202) 555-0123", "+12025550123"],
  ["uk mobile", "+447911123456", "+447911123456"],
  ["spain mobile", "+34612345678", "+34612345678"],
  ["colombia mobile", "+573001234567", "+573001234567"],
  ["peru mobile", "+51987654321", "+51987654321"],
  ["germany with a long national part", "+4915112345678", "+4915112345678"],
  ["minimum four digits", "+1234", "+1234"],
  ["maximum fifteen digits", "+123456789012345", "+123456789012345"],
  ["foreign landline is fine, only Ecuador is strict", "+551133334444", "+551133334444"],
  ["foreign number with a leading zero is kept", "+440987654321", "+440987654321"],

  ["three digits", "+123", null],
  ["sixteen digits", "+1234567890123456", null],
  ["no plus", "2025550123", null],
  ["plus after the digits", "1202555+0123", null],
  ["two pluses", "++12025550123", null],
  ["international dialling prefix instead of a plus", "0012025550123", null],
  ["letters", "+1202555ABCD", null],
  ["extension suffix", "+12025550123 ext 4", null],
  ["plus with no digits", "+", null],
  ["empty", "", null],
  ["whitespace only", "   ", null],
  ["punctuation only", "+()-", null],
  ["fullwidth digits are not digits", "+５９３９８７６５４３２１", null],
  ["arabic-indic digits are not digits", "+٥٩٣", null],
];

for (const [name, input, want] of ECUADOR_CASES) {
  test(`ecuador: ${name}`, () => {
    assert.equal(normalizePhone(input), want);
  });
}

for (const [name, input, want] of GENERIC_CASES) {
  test(`generic: ${name}`, () => {
    assert.equal(normalizePhone(input), want);
  });
}

test("the canonical form is a fixed point", () => {
  // The stored value is re-normalised whenever it comes back out of a profile
  // into the form; a normaliser that changed its own output would corrupt it.
  for (const canonical of ["+593987654321", "+12025550123", "+447911123456"]) {
    assert.equal(normalizePhone(canonical), canonical);
  }
});

test("a blank field is not an error, because the phone is optional", () => {
  // The one place the mirror is knowingly looser than the server, and the
  // correct direction for the two to differ: nothing is sent, so the server's
  // rejection of the empty string is never reached.
  assert.equal(validatePhone(""), null);
  assert.equal(validatePhone("   "), null);
});

test("a rejected number is explained in the terms of its own tier", () => {
  assert.equal(validatePhone("+59322345678"), PHONE_ECUADOR_MESSAGE);
  assert.equal(validatePhone("+59398765"), PHONE_ECUADOR_MESSAGE);
  assert.equal(validatePhone("+593 987 65432a"), PHONE_ECUADOR_MESSAGE);
  assert.equal(validatePhone("+123"), PHONE_GENERIC_MESSAGE);
  assert.equal(validatePhone("+1234567890123456"), PHONE_GENERIC_MESSAGE);
  assert.equal(validatePhone("0987654321"), PHONE_GENERIC_MESSAGE);
});

test("an accepted number has no message at all", () => {
  assert.equal(validatePhone("+593 (0)98 765 4321"), null);
  assert.equal(validatePhone("+12025550123"), null);
});

// The country table (#103). It supplies the prefix for display and carries no
// validation duty, so the assertions here are about the selector a buyer sees.

test("Ecuador is first, because it is the default selection", () => {
  assert.equal(COUNTRIES[0]?.name, "Ecuador");
  assert.equal(COUNTRIES[0]?.diallingCode, ECUADOR_DIALLING_CODE);
});

test("every row is a name and a plus-prefixed dialling code", () => {
  for (const country of COUNTRIES) {
    assert.ok(country.name.length > 0, `unnamed country for ${country.diallingCode}`);
    assert.match(country.diallingCode, /^\+[0-9]{1,4}$/, `bad code for ${country.name}`);
  }
});

test("no country is listed twice", () => {
  const names = COUNTRIES.map((country) => country.name);
  assert.equal(new Set(names).size, names.length);
});

test("the table is long enough to be a real selector", () => {
  // Not a magic number so much as a floor: this is a worldwide field, and a
  // shortlist would be the same as no selector for the buyer it omits.
  assert.ok(COUNTRIES.length > 150);
});

test("the rest of the table is alphabetical, so a long list can be scanned", () => {
  const rest = COUNTRIES.slice(1).map((country) => country.name);
  assert.deepEqual(rest, [...rest].sort((a, b) => a.localeCompare(b, "en")));
});

// The split (#103): a stored canonical number back into the two halves a form
// shows, by longest-prefix match.

test("splitPhone resolves a stored number into a selector value and a field", () => {
  assert.deepEqual(splitPhone("+593987654321"), {
    diallingCode: "+593",
    nationalNumber: "987654321",
  });
  assert.deepEqual(splitPhone("+447911123456"), {
    diallingCode: "+44",
    nationalNumber: "7911123456",
  });
});

test("splitPhone prefers the longest matching dialling code", () => {
  // "+1242" is the Bahamas and "+1" is Canada; the longer prefix must win or
  // every Caribbean number would read as North American.
  assert.deepEqual(splitPhone("+12425551234"), {
    diallingCode: "+1242",
    nationalNumber: "5551234",
  });
});

test("splitPhone resolves a bare +1 to the first +1 row, the accepted imperfection", () => {
  // Documented in phone.ts: +1 covers some twenty countries and the table cannot
  // tell them apart. Cosmetic only — the value sent to PayPhone is untouched.
  const { diallingCode, nationalNumber } = splitPhone("+12025550123");
  assert.equal(diallingCode, "+1");
  assert.equal(nationalNumber, "2025550123");
});

test("splitPhone defaults to Ecuador and an empty field when nothing is stored", () => {
  const empty = { diallingCode: ECUADOR_DIALLING_CODE, nationalNumber: "" };
  assert.deepEqual(splitPhone(null), empty);
  assert.deepEqual(splitPhone(undefined), empty);
  assert.deepEqual(splitPhone(""), empty);
});

test("splitPhone hands back an unrecognised number intact rather than mangling it", () => {
  assert.deepEqual(splitPhone("+9999123456"), {
    diallingCode: "",
    nationalNumber: "+9999123456",
  });
});

test("splitPhone and normalizePhone are inverses over the country table", () => {
  // What a form does on every prefill: split a stored number into the selector
  // and the field, then reassemble it on submit. The round trip must be the
  // identity, or a buyer who opens the dialog and submits without touching the
  // phone field silently changes their own number.
  for (const canonical of ["+593987654321", "+12025550123", "+34612345678", "+12425551234"]) {
    const { diallingCode, nationalNumber } = splitPhone(canonical);
    assert.equal(normalizePhone(`${diallingCode}${nationalNumber}`), canonical);
  }
});
