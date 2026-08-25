import assert from "node:assert/strict";
import test from "node:test";

import {
  CODE128_CODE_B,
  CODE128_START_B,
  CODE128_START_C,
  CODE128_STOP,
  CODE128_WIDTHS,
  code128Modules,
  code128Symbols,
  symbolModules,
} from "./code128.ts";

// The symbol table is transcribed, so its structural invariants are checked
// outright: 107 symbols, every one 11 modules of three bars and three spaces
// (the stop 13 modules of four bars), and an even number of bar modules.
test("the symbol table has the Code 128 shape", () => {
  assert.equal(CODE128_WIDTHS.length, 107);
  for (let value = 0; value < 107; value += 1) {
    const modules = symbolModules(value);
    const isStop = value === CODE128_STOP;
    assert.equal(modules.length, isStop ? 13 : 11, `symbol ${value} width`);
    assert.equal((CODE128_WIDTHS[value] ?? "").length, isStop ? 7 : 6, `symbol ${value} elements`);
    const bars = modules.split("").filter((m) => m === "1").length;
    assert.equal(bars % 2, 0, `symbol ${value} bar parity`);
    assert.ok(modules.startsWith("1"), `symbol ${value} starts with a bar`);
  }
  // Symbols the table is anchored on, from the specification's figures.
  assert.equal(symbolModules(CODE128_START_B), "11010010000");
  assert.equal(symbolModules(CODE128_START_C), "11010011100");
  assert.equal(symbolModules(CODE128_STOP), "1100011101011");
});

// The published example: "PJJ123C" in set B has check character 'W' (55).
test("PJJ123C encodes with check character W", () => {
  assert.deepEqual(code128Symbols("PJJ123C"), [
    CODE128_START_B,
    48, // P
    42, // J
    42, // J
    17, // 1
    18, // 2
    19, // 3
    35, // C
    55, // W, the check character
    CODE128_STOP,
  ]);
});

test("a single letter is Start B, the letter, the check and stop", () => {
  // Start B (104) + 'A' (33) → check (104 + 33) mod 103 = 34 ('B').
  assert.deepEqual(code128Symbols("A"), [CODE128_START_B, 33, 34, CODE128_STOP]);
  assert.equal(code128Modules("A"), "11010010000" + "10100011000" + "10001011000" + "1100011101011");
});

test("an even digit run is set C, two digits per symbol", () => {
  // Start C (105) + 12 + 34 → check (105 + 12·1 + 34·2) mod 103 = 82.
  assert.deepEqual(code128Symbols("1234"), [CODE128_START_C, 12, 34, 82, CODE128_STOP]);
});

test("a 49-digit clave is 48 digits in set C then the last in set B", () => {
  const clave = "0707202601179001234500110010010000000011234567811";
  const symbols = code128Symbols(clave);
  assert.equal(symbols[0], CODE128_START_C);
  assert.equal(symbols.length, 1 + 24 + 1 + 1 + 1 + 1); // start, 24 pairs, Code B, digit, check, stop
  assert.equal(symbols[25], CODE128_CODE_B);
  assert.equal(symbols[26], "1".charCodeAt(0) - 32);
  assert.equal(symbols[symbols.length - 1], CODE128_STOP);
  assert.equal(decode(code128Modules(clave)), clave);
});

test("digit runs switch sets only where it pays, and always decode", () => {
  // Odd run at the start: pairs in C, the last digit in B.
  assert.deepEqual(code128Symbols("12345").slice(0, 5), [CODE128_START_C, 12, 34, CODE128_CODE_B, 21]);
  // A short run inside B stays in B; four or more digits switch to C.
  assert.deepEqual(code128Symbols("AB12CD3456").slice(0, 9), [CODE128_START_B, 33, 34, 17, 18, 35, 36, 99, 34]);
  for (const text of ["12345", "AB12CD3456", "A1", "X12345Y", "1", "PJJ123C"]) {
    assert.equal(decode(code128Modules(text)), text, text);
  }
});

test("refuses what set B cannot carry", () => {
  assert.throws(() => code128Symbols(""), RangeError);
  assert.throws(() => code128Symbols("ñ"), RangeError);
});

// A reader: cuts the modules into symbols, looks each up in the table,
// verifies the check character and walks the code sets back to text. Written
// from the specification independently of the encoder, so the round trip is
// evidence and not tautology.
function decode(modules: string): string {
  const byModules = new Map<string, number>();
  for (let value = 0; value < 107; value += 1) {
    byModules.set(symbolModules(value), value);
  }
  const values: number[] = [];
  let at = 0;
  while (at < modules.length) {
    const width = modules.length - at === 13 ? 13 : 11;
    const value = byModules.get(modules.slice(at, at + width));
    assert.notEqual(value, undefined, `unreadable symbol at module ${at}`);
    values.push(value as number);
    at += width;
  }
  assert.equal(values[values.length - 1], CODE128_STOP);
  const check = values[values.length - 2];
  const data = values.slice(0, -2);
  let sum = data[0] ?? 0;
  for (let k = 1; k < data.length; k += 1) {
    sum += (data[k] ?? 0) * k;
  }
  assert.equal(sum % 103, check, "check character");

  let set = data[0] === CODE128_START_C ? "C" : "B";
  let text = "";
  for (const value of data.slice(1)) {
    if (value === CODE128_CODE_B) {
      set = "B";
    } else if (value === 99) {
      set = "C";
    } else if (set === "C") {
      text += String(value).padStart(2, "0");
    } else {
      text += String.fromCharCode(value + 32);
    }
  }
  return text;
}
