// A small Code 128 encoder (#456): the RIDE prints the clave de acceso as a
// barcode, and the clave is 49 digits, so the encoder favours code set C
// (two digits per symbol) and falls back to set B for anything else,
// including the odd digit a 49-digit clave leaves over.
//
// Output is the module string — "1" for a bar, "0" for a space, one
// character per module — which the SVG component turns into rectangles.
// Nothing here knows about pixels.

/**
 * Symbol patterns 0–106 as bar/space widths (ISO/IEC 15417 Table 1): six
 * widths adding to 11 modules, except the stop (106) which is seven widths
 * adding to 13. Bars and spaces alternate, starting with a bar.
 */
export const CODE128_WIDTHS: readonly string[] = [
  "212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
  "221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
  "221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
  "212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
  "231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
  "231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
  "314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
  "112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
  "111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
  "214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
  "114131", "311141", "411131", "211412", "211214", "211232", "2331112",
];

export const CODE128_START_B = 104;
export const CODE128_START_C = 105;
export const CODE128_CODE_B = 100;
export const CODE128_CODE_C = 99;
export const CODE128_STOP = 106;

/** The modules of one symbol value: bars as "1", spaces as "0". */
export function symbolModules(value: number): string {
  const widths = CODE128_WIDTHS[value];
  if (!widths) {
    throw new RangeError(`code128: no symbol ${value}`);
  }
  let out = "";
  for (let i = 0; i < widths.length; i += 1) {
    out += (i % 2 === 0 ? "1" : "0").repeat(Number(widths[i]));
  }
  return out;
}

/**
 * The symbol values for a text, start code through the check symbol and
 * stop: what a reader recovers from the bars.
 *
 * Digit runs are encoded in set C, two per symbol, when that is cheaper;
 * everything else in set B (printable ASCII 32–127). A 49-digit clave is C
 * for 48 digits then B for the last.
 */
export function code128Symbols(text: string): number[] {
  if (text.length === 0) {
    throw new RangeError("code128: nothing to encode");
  }
  for (const ch of text) {
    const code = ch.charCodeAt(0);
    if (code < 32 || code > 127) {
      throw new RangeError(`code128: character ${JSON.stringify(ch)} is outside set B`);
    }
  }

  const symbols: number[] = [];
  let set: "B" | "C" | null = null;
  let i = 0;
  while (i < text.length) {
    const digits = digitRunLength(text, i);
    // Set C pays for itself at the start or when already in it (two digits
    // per symbol from the first pair); from inside B it needs four digits
    // to cover the switch. A run's odd digit goes in B: the trailing one
    // when the run is taken in C from the start, the leading one when
    // switching out of B, so the pairs that follow are whole either way.
    const useC = digits >= 2 && (set !== "B" || digits >= 4);
    if (useC) {
      if (set === "B" && digits % 2 === 1) {
        symbols.push(text.charCodeAt(i) - 32);
        i += 1;
        continue;
      }
      set = switchTo(symbols, set, "C");
      symbols.push(Number(text.slice(i, i + 2)));
      i += 2;
      continue;
    }
    set = switchTo(symbols, set, "B");
    symbols.push(text.charCodeAt(i) - 32);
    i += 1;
  }

  let check = symbols[0] ?? 0;
  for (let k = 1; k < symbols.length; k += 1) {
    check += (symbols[k] ?? 0) * k;
  }
  symbols.push(check % 103, CODE128_STOP);
  return symbols;
}

/** The module string for a text: every symbol's bars and spaces, in order. */
export function code128Modules(text: string): string {
  return code128Symbols(text).map(symbolModules).join("");
}

function digitRunLength(text: string, from: number): number {
  let n = 0;
  while (from + n < text.length && text.charCodeAt(from + n) >= 48 && text.charCodeAt(from + n) <= 57) {
    n += 1;
  }
  return n;
}

function switchTo(symbols: number[], current: "B" | "C" | null, wanted: "B" | "C"): "B" | "C" {
  if (current === wanted) {
    return current;
  }
  if (current === null) {
    symbols.push(wanted === "C" ? CODE128_START_C : CODE128_START_B);
  } else {
    symbols.push(wanted === "C" ? CODE128_CODE_C : CODE128_CODE_B);
  }
  return wanted;
}
