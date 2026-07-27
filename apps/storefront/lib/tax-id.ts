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

export const TAX_ID_TYPES = ["cedula", "ruc", "passport"] as const;

export type TaxIdType = (typeof TAX_ID_TYPES)[number];

/**
 * The labels a buyer reads. Spanish for the identifier names, because that is
 * what is printed on the documents themselves — an Ecuadorian buyer looks for
 * the word "Cédula" on the card in their hand, not a translation of it — inside
 * an otherwise English Storefront.
 */
export const TAX_ID_TYPE_LABELS: Record<TaxIdType, string> = {
  cedula: "Cédula",
  ruc: "RUC",
  passport: "Pasaporte",
};

export function isTaxIdType(value: string): value is TaxIdType {
  return (TAX_ID_TYPES as readonly string[]).includes(value);
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
 * validateTaxId returns the message to show under the number field, or null when
 * the pair looks good. The messages mirror the API's own wording so the field
 * does not visibly change its mind when the server answers.
 */
export function validateTaxId(taxIdType: string, number: string): string | null {
  const trimmed = number.trim();
  if (trimmed === "") return "is required";
  switch (taxIdType) {
    case "cedula":
      return isValidCedula(trimmed) ? null : "must be a valid 10-digit cédula";
    case "ruc":
      return isValidRuc(trimmed) ? null : "must be a valid 13-digit RUC";
    case "passport":
      return isValidPassport(trimmed) ? null : "must be 6–20 letters or digits";
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
