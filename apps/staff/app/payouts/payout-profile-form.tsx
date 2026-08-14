"use client";

import { FormField, Input } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { ApiError } from "@/lib/events-api";
import { ACCOUNT_TYPES, PAYOUT_TAX_ID_TYPES } from "@/lib/payout-profile";

// The Payout Profile: where this Organization is paid (ADR 0026).
//
// This file used to render a card of its own. It no longer does, and the reason
// is the ADR's: THE REQUEST FORM IS THE PROFILE EDITOR. An organizer correcting
// an account number while asking to be paid means their account number changed,
// and having to fix the same typo in two places is worse than the alternative —
// so there is one set of these six fields on the Payouts page, inside the
// request section, and saving a request saves the profile too. What lives here
// now is the shared material that section is built from: the wire types, the
// fetch that keeps `null` as a legitimate answer, and the fields themselves.
//
// It is Org-Admin-only on the server. The Payouts page is already
// Org-Admin-only, so a refused read here means something has gone wrong rather
// than something ordinary.
//
// Nothing in this file ever logs a bank field or echoes one into a message: the
// account number is the one payload in this system that must not appear in a log
// line or an error (ADR 0026).

export const PAYOUT_PROFILE_PATH = "/api/settings/organization/payout-profile";

/** The stored profile, as the API returns it. `null` when the Organization has never recorded one. */
export type PayoutProfile = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
  updated_at: string;
};

/** The six fields as the form holds them, without the stored row's updated_at. */
export type PayoutProfileFormValues = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
};

/**
 * The empty form. The two enums start on their commonest value rather than
 * blank: a select with no selection is a field an organizer has to notice, and
 * the server refuses either way if the choice is wrong for them.
 */
export const emptyPayoutProfileForm: PayoutProfileFormValues = {
  bank_name: "",
  account_type: "ahorros",
  account_number: "",
  account_holder_name: "",
  tax_id_type: "cedula",
  tax_id_number: "",
};

/** The stored profile as form values. */
export function profileToFormValues(profile: PayoutProfile): PayoutProfileFormValues {
  return {
    bank_name: profile.bank_name,
    account_type: profile.account_type,
    account_number: profile.account_number,
    account_holder_name: profile.account_holder_name,
    tax_id_type: profile.tax_id_type,
    tax_id_number: profile.tax_id_number,
  };
}

export type FieldErrorDetail = { field: string; code: string; message: string };

export type APIEnvelope<T> = {
  data: T | null;
  error: {
    code: string;
    message: string;
    details?: { fields?: FieldErrorDetail[] } & Record<string, unknown>;
  } | null;
};

/**
 * A call that failed field by field. It carries the server's per-field verdicts
 * so each one can be shown beside the input it is about, which is the entire
 * point of the API returning them separately — and it is thrown by the Payout
 * Request submission too, because that endpoint refuses the same fields by the
 * same names.
 *
 * It is an ApiError so that one `catch` can hand either kind of refusal to
 * `apiErrorMessage`, and so the `details` the field copy is resolved from
 * survive the throw.
 */
export class FieldValidationError extends ApiError {
  constructor(message: string, code?: string, details?: Record<string, unknown>) {
    super(message, code, details);
    this.name = "FieldValidationError";
  }
}

/**
 * Turns an envelope's error into the right kind of Error, or returns its data.
 *
 * BOTH KINDS CARRY THE API's CODE AND DETAILS RATHER THAN ITS SENTENCE, because
 * the sentence is the caller's to choose from the catalog now (ADR 0023,
 * ADR 0041). A field-level refusal keeps its whole `details` payload instead of
 * being flattened here into field→message: `fieldErrorMessages` resolves each
 * field's CODE against `errors.field` at the call site, with the API's English
 * as the floor under any code the catalog has not heard of, and flattening early
 * would throw away the one thing that resolution keys on.
 */
export function unwrapEnvelope<T>(response: Response, envelope: APIEnvelope<T>): T | null {
  if (!response.ok || envelope.error) {
    const fields = envelope.error?.details?.fields ?? [];
    if (fields.length > 0) {
      throw new FieldValidationError(
        envelope.error?.message ?? "",
        envelope.error?.code,
        envelope.error?.details,
      );
    }
    throw new ApiError(envelope.error?.message ?? "", envelope.error?.code, envelope.error?.details);
  }
  return envelope.data;
}

/**
 * Reads or writes the profile, keeping `null` data as a legitimate answer: an
 * Organization that has never recorded a Payout Profile is not an error.
 */
export async function requestProfile(init?: RequestInit): Promise<PayoutProfile | null> {
  const response = await fetch(PAYOUT_PROFILE_PATH, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<PayoutProfile>;
  return unwrapEnvelope(response, envelope);
}

/**
 * The six bank fields, with each server refusal shown under the input it is
 * about. Presentational: it owns no state and performs no request, so the same
 * markup serves the profile-only save and the request submission that saves the
 * profile as a side effect.
 */
export function PayoutProfileFields({
  values,
  errors,
  onChange,
  disabled,
}: {
  values: PayoutProfileFormValues;
  errors: Record<string, string>;
  onChange: (field: keyof PayoutProfileFormValues, value: string) => void;
  disabled?: boolean;
}) {
  const t = useTranslations("payouts");

  // The two enums are rendered from the tokens the API states them in, with the
  // catalog holding the word for each — the same shape a status badge uses. Both
  // catalogs happen to say "Ahorros" and "Corriente", because those are the words
  // on the Ecuadorian bank form an organizer is copying from, and translating
  // them would be asking a reader to translate their own bank back.
  return (
    <div className="space-y-4">
      <FormField id="payout-bank-name" label={t("fieldBank")} error={errors.bank_name}>
        <Input
          value={values.bank_name}
          disabled={disabled}
          onChange={(event) => onChange("bank_name", event.target.value)}
          placeholder={t("fieldBankPlaceholder")}
        />
      </FormField>

      <FormField id="payout-account-type" label={t("fieldAccountType")} error={errors.account_type}>
        <select
          id="payout-account-type"
          className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60"
          value={values.account_type}
          disabled={disabled}
          onChange={(event) => onChange("account_type", event.target.value)}
        >
          {ACCOUNT_TYPES.map((accountType) => (
            <option key={accountType} value={accountType}>
              {accountType === "ahorros" ? t("accountTypeAhorros") : t("accountTypeCorriente")}
            </option>
          ))}
        </select>
      </FormField>

      <FormField
        id="payout-account-number"
        label={t("fieldAccountNumber")}
        description={t("fieldAccountNumberHint")}
        error={errors.account_number}
      >
        <Input
          inputMode="numeric"
          value={values.account_number}
          disabled={disabled}
          onChange={(event) => onChange("account_number", event.target.value)}
        />
      </FormField>

      <FormField
        id="payout-account-holder-name"
        label={t("fieldAccountHolder")}
        description={t("fieldAccountHolderHint")}
        error={errors.account_holder_name}
      >
        <Input
          value={values.account_holder_name}
          disabled={disabled}
          onChange={(event) => onChange("account_holder_name", event.target.value)}
        />
      </FormField>

      <FormField
        id="payout-tax-id-type"
        label={t("fieldTaxIdType")}
        description={t("fieldTaxIdTypeHint")}
        error={errors.tax_id_type}
      >
        <select
          id="payout-tax-id-type"
          className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60"
          value={values.tax_id_type}
          disabled={disabled}
          onChange={(event) => onChange("tax_id_type", event.target.value)}
        >
          {PAYOUT_TAX_ID_TYPES.map((taxIDType) => (
            <option key={taxIDType} value={taxIDType}>
              {taxIDType === "cedula" ? t("taxIdTypeCedula") : t("taxIdTypeRuc")}
            </option>
          ))}
        </select>
      </FormField>

      <FormField
        id="payout-tax-id-number"
        label={t("fieldTaxIdNumber")}
        error={errors.tax_id_number}
      >
        <Input
          inputMode="numeric"
          value={values.tax_id_number}
          disabled={disabled}
          onChange={(event) => onChange("tax_id_number", event.target.value)}
        />
      </FormField>
    </div>
  );
}
