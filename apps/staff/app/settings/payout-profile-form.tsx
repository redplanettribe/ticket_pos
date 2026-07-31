"use client";

import { useCallback, useEffect, useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  toast,
} from "@ticket-pos/ui";

import { maskAccountNumber, normalizeAccountNumber } from "@/lib/payout-profile";

// The Payout Profile editor: where this Organization is paid (ADR 0025).
//
// It is the whole of the Organization's side of a settlement. Everything on it
// exists to spare a person retyping bank details into a WhatsApp thread every
// month, so the form is deliberately plain: no wizard, no confirm-by-retyping —
// the profile is stored, visible and editable, and a human reads it before any
// money moves.
//
// It is Org-Admin-only on the server. This component is rendered inside the
// Settings page, which is already Org-Admin-only, so a refused read here means
// something has gone wrong rather than something ordinary — the card says so and
// stays out of the way.

const PAYOUT_PROFILE_PATH = "/api/settings/organization/payout-profile";

type PayoutProfile = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
  updated_at: string;
};

type FieldErrorDetail = { field: string; code: string; message: string };

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: { fields?: FieldErrorDetail[] } } | null;
};

/**
 * A save that failed field by field. It carries the server's per-field verdicts
 * so each one can be shown beside the input it is about, which is the entire
 * point of the API returning them separately.
 */
class FieldValidationError extends Error {
  fields: Record<string, string>;

  constructor(message: string, fields: Record<string, string>) {
    super(message);
    this.fields = fields;
  }
}

/**
 * Reads the envelope, keeping `null` data as a legitimate answer: an
 * Organization that has never recorded a Payout Profile is not an error.
 */
async function requestProfile(init?: RequestInit): Promise<PayoutProfile | null> {
  const response = await fetch(PAYOUT_PROFILE_PATH, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<PayoutProfile>;
  if (!response.ok || envelope.error) {
    const fields = envelope.error?.details?.fields ?? [];
    if (fields.length > 0) {
      throw new FieldValidationError(
        envelope.error?.message ?? "Request failed",
        Object.fromEntries(fields.map((field) => [field.field, field.message])),
      );
    }
    throw new Error(envelope.error?.message ?? "Request failed");
  }
  return envelope.data;
}

const emptyForm = {
  bank_name: "",
  account_type: "ahorros",
  account_number: "",
  account_holder_name: "",
  tax_id_type: "cedula",
  tax_id_number: "",
};

export function PayoutProfileForm() {
  const [form, setForm] = useState(emptyForm);
  const [saved, setSaved] = useState<PayoutProfile | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const profile = await requestProfile();
      setSaved(profile);
      if (profile) {
        setForm({
          bank_name: profile.bank_name,
          account_type: profile.account_type,
          account_number: profile.account_number,
          account_holder_name: profile.account_holder_name,
          tax_id_type: profile.tax_id_type,
          tax_id_number: profile.tax_id_number,
        });
      }
      setLoadError(null);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Failed to load the payout profile");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  function update(field: keyof typeof emptyForm, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
    // A field being corrected stops carrying its old refusal, so the message
    // under it always describes what is in it now.
    setFieldErrors((current) => {
      if (!current[field]) {
        return current;
      }
      const rest = { ...current };
      delete rest[field];
      return rest;
    });
  }

  async function save() {
    setSaving(true);
    setFieldErrors({});
    try {
      // The account number is normalised before it is sent so the field shows
      // what will be stored, rather than the organizer discovering on the next
      // load that their dashes went away.
      const body = { ...form, account_number: normalizeAccountNumber(form.account_number) };
      const profile = await requestProfile({ method: "PUT", body: JSON.stringify(body) });
      setSaved(profile);
      setForm((current) => ({ ...current, account_number: body.account_number }));
      toast.success("Payout profile saved");
    } catch (error) {
      if (error instanceof FieldValidationError) {
        setFieldErrors(error.fields);
        toast.error("Check the highlighted fields");
      } else {
        toast.error(error instanceof Error ? error.message : "Failed to save the payout profile");
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Payout profile</CardTitle>
        <CardDescription>
          Where this organization is paid. Only Org Admins can see or change it, and it is never shown to
          Event staff or to your customers.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading payout profile...</p>
        ) : loadError ? (
          <p className="text-sm text-destructive">{loadError}</p>
        ) : (
          <>
            {saved ? (
              <p className="text-sm text-muted-foreground">
                Currently paying to {saved.bank_name} {maskAccountNumber(saved.account_number)}, last
                updated {new Date(saved.updated_at).toLocaleDateString()}.
              </p>
            ) : (
              <p className="text-sm text-muted-foreground">
                No payout profile yet. Add one so a payout does not have to start with a message asking for
                your bank details.
              </p>
            )}

            <FormField id="payout-bank-name" label="Bank" error={fieldErrors.bank_name}>
              <Input
                value={form.bank_name}
                onChange={(event) => update("bank_name", event.target.value)}
                placeholder="Banco Pichincha"
              />
            </FormField>

            <FormField id="payout-account-type" label="Account type" error={fieldErrors.account_type}>
              <select
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={form.account_type}
                onChange={(event) => update("account_type", event.target.value)}
              >
                <option value="ahorros">Ahorros</option>
                <option value="corriente">Corriente</option>
              </select>
            </FormField>

            <FormField
              id="payout-account-number"
              label="Account number"
              description="Digits only. Leading zeros matter and are kept exactly as you type them."
              error={fieldErrors.account_number}
            >
              <Input
                inputMode="numeric"
                value={form.account_number}
                onChange={(event) => update("account_number", event.target.value)}
              />
            </FormField>

            <FormField
              id="payout-account-holder-name"
              label="Account holder"
              description="The name on the account, as the bank has it."
              error={fieldErrors.account_holder_name}
            >
              <Input
                value={form.account_holder_name}
                onChange={(event) => update("account_holder_name", event.target.value)}
              />
            </FormField>

            <FormField
              id="payout-tax-id-type"
              label="Tax ID type"
              description="Who is invoiced for the payout. A passport is not accepted here — the account holder needs an Ecuadorian bank account."
              error={fieldErrors.tax_id_type}
            >
              <select
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={form.tax_id_type}
                onChange={(event) => update("tax_id_type", event.target.value)}
              >
                <option value="cedula">Cédula</option>
                <option value="ruc">RUC</option>
              </select>
            </FormField>

            <FormField id="payout-tax-id-number" label="Tax ID number" error={fieldErrors.tax_id_number}>
              <Input
                inputMode="numeric"
                value={form.tax_id_number}
                onChange={(event) => update("tax_id_number", event.target.value)}
              />
            </FormField>

            <Button type="button" disabled={saving} onClick={() => void save()}>
              {saving ? "Saving..." : "Save payout profile"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
