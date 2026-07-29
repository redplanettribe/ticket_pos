/**
 * API error copy, chosen by the API's own error code (ADR 0023).
 *
 * The Go API answers in English and knows nothing about Locale. What it does
 * carry is `error.code` — its stable statement of WHICH failure occurred — and
 * that is what the catalog keys its sentence on. The Storefront therefore
 * re-renders the API's verdict in the language the page is being read in; it
 * never re-decides which failure applies, which is the thing the "show
 * error.message verbatim" rule existed to prevent (docs/design/foundation.md).
 *
 * The fallback is the load-bearing half. A backend that starts sending a code
 * this catalog has never heard of degrades to the API's own message — exactly
 * what every one of these surfaces rendered before this module existed. So a new
 * code ships from the API alone, with no coordinated Storefront release, and the
 * window between the two shows an English sentence rather than a blank alert.
 *
 * Nothing here imports next-intl, React, or the catalogs. The caller hands in
 * the `errors` namespace as plain data — `useMessages().errors` in a client
 * component, `(await getMessages()).errors` on the server — which is what lets
 * the whole of the resolution be unit-tested against the real en.json.
 */

/**
 * The `errors` namespace of a message catalog: groups of copy, each keyed by an
 * API code.
 *
 * Deliberately loose about which groups exist. The compiler already holds
 * en.json to its shape through global.d.ts, and a resolver that insisted on the
 * groups being present could not survive the one thing it is built for — a
 * catalog that does not know about a code yet.
 */
export type ErrorCatalog = Readonly<Record<string, Readonly<Record<string, string>> | undefined>>;

/**
 * A surface that changes what a code means.
 *
 * Almost every code says the same thing wherever it lands, so almost every code
 * is looked up in `envelope` alone. `CUSTOMER_SESSION_SCOPE_INSUFFICIENT` is the
 * exception the API itself makes: one code, two messages, chosen by the
 * operation that was refused — "sign in with a passcode to undo this purchase"
 * against "…to change your details" (backend/internal/customers/errors.go). A
 * key of code alone could only pick one of them and would be wrong half the
 * time, so the call site names the surface and the surface group is consulted
 * first.
 *
 * A surface group holds only the codes whose meaning that surface changes.
 * Everything else falls through to `envelope`, and a code no group claims falls
 * through to the API's message — so a third surface that starts receiving an
 * ambiguous code shows the API's own sentence rather than another surface's.
 */
export type ErrorSurface = "undo" | "myInfo";

/** As much of a failed envelope's `error` as choosing copy needs. */
export type ApiError = { code?: string | null; message?: string | null } | null | undefined;

/**
 * The field codes the client-side mirror validators answer with — lib/tax-id.ts,
 * lib/phone.ts and validateProfileDraft, which reach a verdict before the API is
 * asked at all.
 *
 * Every one of them is a code the API itself sends
 * (backend/internal/platform/validation_codes.go), deliberately: a mirror that
 * invented its own vocabulary would resolve through a second set of catalog
 * entries, and the two sets would drift into telling a buyer two different
 * things about one mistyped digit. Sharing the code is what makes the pre-flight
 * and post-flight sentences the same sentence rather than two sentences that
 * happen to match today.
 *
 * A list rather than a bare union so the catalogs can be held to it — every code
 * here must have copy in every language, since a mirror has no API message to
 * fall back on (see fieldCodeMessage).
 */
export const MIRROR_FIELD_CODES = [
  "REQUIRED",
  "INVALID_CEDULA",
  "INVALID_RUC",
  "INVALID_PASSPORT",
  "INVALID_PHONE_EC",
  "INVALID_PHONE",
] as const;

export type FieldErrorCode = (typeof MIRROR_FIELD_CODES)[number];

/**
 * fieldCodeMessage resolves ONE field code to the sentence shown under the
 * input — the mirrors' half of the same path fieldErrorMessages walks for the
 * API's own field errors, keyed on `field` in the same catalog group.
 *
 * The two must agree, and they agree by construction: one code, one lookup, one
 * sentence. A cédula rejected here reads exactly as a cédula rejected by the API
 * reads, which is the whole reason the mirrors stopped carrying English
 * sentences of their own (ADR 0023).
 *
 * The last resort is the code itself, not null and not a fallback message. There
 * is no API message to fall back to — nothing was sent — and a caller handed
 * null would block the submit while showing an empty error line, which is a form
 * refusing without saying why. Unreachable in practice: the codes are this app's
 * own closed set, and api-errors.test.ts holds the catalog to all of them.
 */
export function fieldCodeMessage(catalog: ErrorCatalog, code: FieldErrorCode): string {
  return catalog.field?.[code] ?? code;
}

/**
 * usableMessage is the guard that keeps a fallback from rendering as nothing.
 *
 * An absent message, and a message that is only whitespace, are the same thing
 * to a reader: a destructive Alert with an empty body. Both answer null so the
 * call site can show its own sentence instead. The string itself is returned
 * untouched — the API's wording is the API's, and trimming it here would be the
 * first step of rewriting it.
 */
function usableMessage(message: unknown): string | null {
  return typeof message === "string" && message.trim() !== "" ? message : null;
}

/**
 * apiErrorMessage returns the sentence to show for a failed envelope: this
 * app's copy for the code when it has some, the API's own message otherwise,
 * and null when there is nothing to show at all.
 *
 * Null is not a failure — it is the answer for a call that never reached the API
 * (a dropped connection) or one whose envelope carried no words. The caller
 * holds its own sentence for that case, because only the caller knows what was
 * being attempted.
 */
export function apiErrorMessage(
  catalog: ErrorCatalog,
  error: ApiError,
  surface?: ErrorSurface,
): string | null {
  if (!error) return null;
  const code = typeof error.code === "string" ? error.code : null;
  if (code !== null) {
    const copy = (surface ? catalog[surface]?.[code] : undefined) ?? catalog.envelope?.[code];
    if (copy) return copy;
  }
  return usableMessage(error.message);
}

/**
 * fieldErrorMessages picks the field-level errors out of a VALIDATION_FAILED
 * envelope's `details`, keyed by the API's own field name.
 *
 * Same rule one level down: the FieldError's `code` names the RULE that failed
 * ("REQUIRED", "INVALID_CEDULA") and is stable, while its `message` is a
 * sentence that gets reworded (backend/internal/platform/validation_codes.go).
 * A field whose code the catalog does not know keeps the API's message, and a
 * field with neither is dropped rather than marked with an empty line.
 *
 * Every field the API named is returned, including ones no form on this
 * Storefront has an input for. Filtering to a particular form's inputs belongs
 * to that form, which is the only thing that knows what it drew.
 */
export function fieldErrorMessages(
  catalog: ErrorCatalog,
  details: unknown,
): Record<string, string> {
  const errors: Record<string, string> = {};
  if (typeof details !== "object" || details === null) return errors;
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) return errors;
  for (const entry of fields) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, code, message } = entry as {
      field?: unknown;
      code?: unknown;
      message?: unknown;
    };
    if (typeof field !== "string" || field === "") continue;
    const copy =
      (typeof code === "string" ? catalog.field?.[code] : undefined) ?? usableMessage(message);
    if (copy) errors[field] = copy;
  }
  return errors;
}
