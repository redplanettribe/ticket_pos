/**
 * The Customer Area's "My info" profile (#102): what the form holds, what it
 * sends, and the mirror validation that answers before the API does.
 *
 * The rules here are the API's rules restated for instant feedback, exactly as
 * lib/tax-id.ts is for the checkout dialog — the server's verdict is still the
 * one that decides whether an edit lands. Two of them are worth naming because
 * they are not obvious from the form:
 *
 * - A blank first or last name is refused. The API refuses it too, and for a
 *   reason that reaches past this screen: its customer-upsert guard reads a
 *   blank name as "never named", so a name blanked here would be silently
 *   rewritten by the person's next purchase.
 * - An empty ID number means "clear my Tax ID", not "I forgot to type it". Both
 *   halves travel as null together — a Tax ID is one fact split across two
 *   controls, never half stored.
 */

// The ".ts" is written out because these unit tests run under
// `node --experimental-strip-types`, whose resolution needs it (as
// lib/google-signin.ts does for the same reason).
import { normalizeTaxIdNumber, validateTaxId, type TaxIdType } from "./tax-id.ts";

/** The Customer's own record as the API returns it from the profile endpoint. */
export type CustomerProfile = {
  email: string;
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
};

/** The API's profile field names, which are also the form's inputs. */
export const PROFILE_FIELDS = ["first_name", "last_name", "tax_id_type", "tax_id_number"] as const;

export type ProfileField = (typeof PROFILE_FIELDS)[number];

export type ProfileFieldErrors = Partial<Record<ProfileField, string>>;

/** What the form holds while it is being edited. */
export type ProfileDraft = {
  firstName: string;
  lastName: string;
  taxIdType: TaxIdType;
  taxIdNumber: string;
};

/** The PATCH body: the editable fields, and deliberately no email. */
export type ProfileUpdate = {
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
};

function isProfileField(field: string): field is ProfileField {
  return (PROFILE_FIELDS as readonly string[]).includes(field);
}

/**
 * validateProfileDraft returns the field errors to show, or an empty object when
 * the draft is ready to send. The messages mirror the API's own wording so a
 * field does not visibly change its mind when the server answers.
 */
export function validateProfileDraft(draft: ProfileDraft): ProfileFieldErrors {
  const errors: ProfileFieldErrors = {};
  if (draft.firstName.trim() === "") errors.first_name = "is required";
  if (draft.lastName.trim() === "") errors.last_name = "is required";

  // An empty number is the clear, and clearing is always allowed.
  if (draft.taxIdNumber.trim() !== "") {
    const problem = validateTaxId(draft.taxIdType, draft.taxIdNumber);
    if (problem) errors.tax_id_number = problem;
  }
  return errors;
}

/**
 * profileUpdateBody turns a draft into the request body: names trimmed, and the
 * Tax ID either both halves normalised or both halves null.
 */
export function profileUpdateBody(draft: ProfileDraft): ProfileUpdate {
  const number = draft.taxIdNumber.trim();
  const cleared = number === "";
  return {
    first_name: draft.firstName.trim(),
    last_name: draft.lastName.trim(),
    tax_id_type: cleared ? null : draft.taxIdType,
    tax_id_number: cleared ? null : normalizeTaxIdNumber(draft.taxIdType, number),
  };
}

/**
 * profileFieldErrorsFromDetails picks the field-level errors out of the API's
 * VALIDATION_FAILED envelope, ignoring any field this form has no input for.
 */
export function profileFieldErrorsFromDetails(details: unknown): ProfileFieldErrors {
  const errors: ProfileFieldErrors = {};
  if (typeof details !== "object" || details === null) return errors;
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) return errors;
  for (const entry of fields) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, message } = entry as { field?: unknown; message?: unknown };
    if (typeof field !== "string" || typeof message !== "string") continue;
    if (isProfileField(field)) errors[field] = message;
  }
  return errors;
}
