/**
 * The Tax ID as the checkout dialog sees it (#98, ADR 0016): the three Tax ID
 * Types, their buyer-facing labels, and a *mirror* of the API's validation.
 *
 * Mirror is the operative word. `backend/internal/platform/taxid.go` is the
 * single source of truth for what counts as a valid Tax ID, and the server's
 * verdict is the one that decides whether a sale is recorded. What lives here
 * exists only so a buyer who fat-fingers a digit is told immediately instead of
 * after a round trip — so it must never be *stricter* than the backend (which
 * would lock out a legitimate buyer the API would have accepted), and when the
 * two disagree the field error the API returns wins on screen.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import type { FieldErrorCode } from "./api-errors.ts";

export const TAX_ID_TYPES = ["cedula", "ruc", "passport"] as const;

export type TaxIdType = (typeof TAX_ID_TYPES)[number];

/**
 * The labels a buyer reads, and the one thing on this Storefront that is the
 * same in both languages. They are the names printed on the documents
 * themselves — an Ecuadorian buyer looks for the word "Cédula" on the card in
 * their hand, not a translation of it — so they are not in the catalogs at all.
 */
export const TAX_ID_TYPE_LABELS: Record<TaxIdType, string> = {
  cedula: "Cédula",
  ruc: "RUC",
  passport: "Pasaporte",
};

export function isTaxIdType(value: string): value is TaxIdType {
  return (TAX_ID_TYPES as readonly string[]).includes(value);
}

/**
 * formatTaxId renders a Ticket Sale's Tax ID snapshot the way the buyer's own
 * receipt shows it — "Cédula: 1712345678" — or null when the sale carries none
 * (#99). Callers draw "—" for null rather than an empty label.
 *
 * The two halves are always null together on the wire, but this treats a
 * half-populated pair as absent anyway: half a Tax ID identifies nobody, and a
 * receipt is the wrong place to guess at the other half.
 *
 * Unmasked, deliberately. This is the value a Customer copies into their own
 * expense records, and it is shown only to the person the sale belongs to —
 * the Customer Area is reachable with nothing but their own session.
 */
export function formatTaxId(taxIdType: string | null, number: string | null): string | null {
  if (!taxIdType || !number) return null;
  const label = isTaxIdType(taxIdType) ? TAX_ID_TYPE_LABELS[taxIdType] : taxIdType;
  return `${label}: ${number}`;
}

/** Province codes 01–24, plus 30 for citizens registered abroad. */
function hasValidProvincePrefix(number: string): boolean {
  const province = Number(number.slice(0, 2));
  return (province >= 1 && province <= 24) || province === 30;
}

function isDigits(value: string): boolean {
  return value.length > 0 && /^[0-9]+$/.test(value);
}

/**
 * The cédula's modulo-10 check digit over the first nine digits: coefficients
 * 2,1,2,1,…, any product above 9 reduced by 9, and the digit that completes the
 * sum to the next multiple of ten.
 */
function cedulaCheckDigit(number: string): string {
  let sum = 0;
  for (let i = 0; i < 9; i += 1) {
    let digit = Number(number[i]);
    if (i % 2 === 0) {
      digit *= 2;
      if (digit > 9) digit -= 9;
    }
    sum += digit;
  }
  return String((10 - (sum % 10)) % 10);
}

function isValidCedula(number: string): boolean {
  if (number.length !== 10 || !isDigits(number)) return false;
  if (!hasValidProvincePrefix(number)) return false;
  return number[9] === cedulaCheckDigit(number);
}

/**
 * Thirteen digits, a real province prefix, and a third digit naming one of the
 * three RUC forms. Only the natural-person form (third digit 0–5) is checked
 * further — it is a cédula with an establishment suffix — matching the backend's
 * gradient exactly.
 */
function isValidRuc(number: string): boolean {
  if (number.length !== 13 || !isDigits(number)) return false;
  if (!hasValidProvincePrefix(number)) return false;
  const third = Number(number[2]);
  if (third >= 0 && third <= 5) return number[9] === cedulaCheckDigit(number);
  return third === 6 || third === 9;
}

/** Permissive by design: any country's scheme, so shape alone. */
function isValidPassport(number: string): boolean {
  return /^[A-Za-z0-9]{6,20}$/.test(number);
}

/**
 * validateTaxId names the rule the number broke, or null when the pair looks
 * good. It returns the API's own field code rather than a sentence, so the
 * caller resolves it through the same catalog entry the API's field error
 * resolves through and the field cannot say one thing before the round trip and
 * another after it — in English before and in Spanish after, which is what a
 * sentence written here meant once the Storefront had two languages (ADR 0022).
 *
 * The verdicts are unchanged; only their wording moved. This module stays pure —
 * no next-intl, no translator argument — because the rules are what is worth
 * unit-testing and copy is not.
 */
export function validateTaxId(taxIdType: string, number: string): FieldErrorCode | null {
  const trimmed = number.trim();
  if (trimmed === "") return "REQUIRED";
  switch (taxIdType) {
    case "cedula":
      return isValidCedula(trimmed) ? null : "INVALID_CEDULA";
    case "ruc":
      return isValidRuc(trimmed) ? null : "INVALID_RUC";
    case "passport":
      return isValidPassport(trimmed) ? null : "INVALID_PASSPORT";
    default:
      // An unknown type is the type field's problem, not the number's.
      return null;
  }
}

/**
 * normalizeTaxIdNumber puts the number in the form the API stores: trimmed
 * always, uppercased for passports so "ab123456" and "AB123456" are one
 * passport. Sending the normalised value keeps what the buyer sees echoed back
 * on their confirmation identical to what was typed here.
 */
export function normalizeTaxIdNumber(taxIdType: string, number: string): string {
  const trimmed = number.trim();
  return taxIdType === "passport" ? trimmed.toUpperCase() : trimmed;
}
