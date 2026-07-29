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
 * - An empty phone field means the same thing: remove the stored number. A
 *   Customer may withdraw a detail they no longer wish held (#103, user story
 *   10), so clearing is a capability the form has to offer rather than an edge
 *   case it has to survive.
 */

// The ".ts" is written out because these unit tests run under
// `node --experimental-strip-types`, whose resolution needs it (as
// lib/google-signin.ts does for the same reason).
import {
  fieldCodeMessage,
  fieldErrorMessages,
  type ErrorCatalog,
  type FieldErrorCode,
} from "./api-errors.ts";
import {
  ECUADOR_DIALLING_CODE,
  composePhone,
  normalizePhone,
  splitPhone,
  validatePhone,
} from "./phone.ts";
import { normalizeTaxIdNumber, validateTaxId, type TaxIdType } from "./tax-id.ts";

/** The Customer's own record as the API returns it from the profile endpoint. */
export type CustomerProfile = {
  email: string;
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
  /**
   * The stored phone in canonical E.164 form, null when the Customer has none.
   * Canonical is the only form that crosses the wire; splitting it into a
   * dialling code and a national number is this app's business and nobody
   * else's (#103).
   */
  phone: string | null;
  /**
   * The Customer's Avatar as a browser-loadable URL, null when they have none.
   * Read-only on the profile PATCH — attaching and removing go through the
   * dedicated avatar routes.
   */
  avatar_url: string | null;
};

/** The API's profile field names, which are also the form's inputs. */
export const PROFILE_FIELDS = [
  "first_name",
  "last_name",
  "tax_id_type",
  "tax_id_number",
  "phone",
] as const;

export type ProfileField = (typeof PROFILE_FIELDS)[number];

/** The sentences the form shows under its inputs, keyed by field. */
export type ProfileFieldErrors = Partial<Record<ProfileField, string>>;

/**
 * The same fields, holding the API's field CODE instead of a sentence — what the
 * mirror validation answers with before anything is sent. Copy is chosen from it
 * at render, by profileFieldMessages, so nothing here is language-bound.
 */
export type ProfileFieldCodes = Partial<Record<ProfileField, FieldErrorCode>>;

/**
 * What the form holds while it is being edited.
 *
 * The phone is two controls here and one value on the wire, exactly as it is in
 * the checkout dialog: a person picks their country and types the number they
 * would read off their own screen, and the pair assembles into the single
 * canonical string everything below the form deals in.
 */
export type ProfileDraft = {
  firstName: string;
  lastName: string;
  taxIdType: TaxIdType;
  taxIdNumber: string;
  phoneDiallingCode: string;
  phoneNationalNumber: string;
};

/** The PATCH body: the editable fields, and deliberately no email. */
export type ProfileUpdate = {
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
  /**
   * Null clears the stored number. The key is always sent, because this form
   * always knows what the Customer means about their phone — the API's "say
   * nothing and I leave it alone" reading is there for clients that predate the
   * field, not for this one (#108).
   */
  phone: string | null;
};

function isProfileField(field: string): field is ProfileField {
  return (PROFILE_FIELDS as readonly string[]).includes(field);
}

/**
 * validateProfileDraft returns the rule each field broke, keyed by field, or an
 * empty object when the draft is ready to send.
 *
 * Codes, not sentences: the same codes the API's own field errors carry, so a
 * verdict reached here and the identical verdict reached by the API resolve to
 * one sentence in whichever language the page is being read in (ADR 0022). The
 * rules are exactly the ones this function has always applied.
 */
export function validateProfileDraft(draft: ProfileDraft): ProfileFieldCodes {
  const errors: ProfileFieldCodes = {};
  if (draft.firstName.trim() === "") errors.first_name = "REQUIRED";
  if (draft.lastName.trim() === "") errors.last_name = "REQUIRED";

  // An empty number is the clear, and clearing is always allowed.
  if (draft.taxIdNumber.trim() !== "") {
    const problem = validateTaxId(draft.taxIdType, draft.taxIdNumber);
    if (problem) errors.tax_id_number = problem;
  }

  // The phone runs the shared rule, and an empty field is likewise the clear
  // rather than a complaint. validatePhone already answers null for a blank, so
  // this passes the assembled value straight through: the emptiness is decided by
  // draftPhone below, in one place, and not restated here.
  const problem = validatePhone(draftPhone(draft));
  if (problem) errors.phone = problem;

  return errors;
}

/**
 * draftPhone reads the draft's two phone controls as the one string the rule is
 * written against. The assembly itself is composePhone in lib/phone.ts, shared
 * with the checkout dialog: the same two controls appear on both surfaces, and a
 * second copy of the rule for joining them is a second place for them to
 * disagree about what a pasted international number means.
 */
function draftPhone(draft: ProfileDraft): string {
  return composePhone(draft.phoneDiallingCode, draft.phoneNationalNumber);
}

/**
 * profileUpdateBody turns a draft into the request body: names trimmed, the Tax
 * ID either both halves normalised or both halves null, and the phone canonical
 * or null.
 *
 * It must only be called on a draft that has passed validateProfileDraft — a
 * phone that does not normalise would arrive here as null, which is the clear,
 * and silently erasing a number because the buyer mistyped it would be the worst
 * possible reading of what they meant.
 */
export function profileUpdateBody(draft: ProfileDraft): ProfileUpdate {
  const number = draft.taxIdNumber.trim();
  const cleared = number === "";
  const phone = draftPhone(draft);
  return {
    first_name: draft.firstName.trim(),
    last_name: draft.lastName.trim(),
    tax_id_type: cleared ? null : draft.taxIdType,
    tax_id_number: cleared ? null : normalizeTaxIdNumber(draft.taxIdType, number),
    phone: phone === "" ? null : normalizePhone(phone),
  };
}

/**
 * profileDraftPhone is the other direction: a stored canonical number resolved
 * back into the two controls the form shows, by longest-prefix match against the
 * country table.
 *
 * A Customer with no stored number gets Ecuador and an empty field — the same
 * state the checkout dialog opens in, because Ecuador is the home market and the
 * common case should need no interaction at all. A number whose dialling code is
 * no longer in the table keeps Ecuador on the selector while the field shows the
 * value intact, so nothing a buyer stored is ever mangled to fit a control.
 */
export function profileDraftPhone(phone: string | null): {
  phoneDiallingCode: string;
  phoneNationalNumber: string;
} {
  const { diallingCode, nationalNumber } = splitPhone(phone);
  return {
    phoneDiallingCode: diallingCode === "" ? ECUADOR_DIALLING_CODE : diallingCode,
    phoneNationalNumber: nationalNumber,
  };
}

/**
 * profileFieldMessages is the mirror's half of the copy: the codes
 * validateProfileDraft answered with, each resolved to the sentence its field
 * shows.
 *
 * It sits beside profileFieldErrorsFromDetails deliberately. The two are the
 * before and after of one round trip, they resolve through the same catalog
 * group on the same codes, and a reader comparing them should be able to see at
 * a glance that neither has its own opinion about wording.
 */
export function profileFieldMessages(
  catalog: ErrorCatalog,
  codes: ProfileFieldCodes,
): ProfileFieldErrors {
  const errors: ProfileFieldErrors = {};
  for (const [field, code] of Object.entries(codes)) {
    if (code && isProfileField(field)) errors[field] = fieldCodeMessage(catalog, code);
  }
  return errors;
}

/**
 * profileFieldErrorsFromDetails picks the field-level errors out of the API's
 * VALIDATION_FAILED envelope, ignoring any field this form has no input for.
 *
 * The copy itself is chosen by fieldErrorMessages, on the FieldError's stable
 * `code`, falling back to its message (ADR 0022). All this adds is the filter,
 * which is the one part that belongs to the form: a field with no input to sit
 * under has nowhere to render.
 */
export function profileFieldErrorsFromDetails(
  catalog: ErrorCatalog,
  details: unknown,
): ProfileFieldErrors {
  const errors: ProfileFieldErrors = {};
  for (const [field, message] of Object.entries(fieldErrorMessages(catalog, details))) {
    if (isProfileField(field)) errors[field] = message;
  }
  return errors;
}
