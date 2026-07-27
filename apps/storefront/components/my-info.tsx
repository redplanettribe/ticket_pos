"use client";

import { Alert, AlertDescription, Button, FormField, Input, toast } from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import {
  profileFieldErrorsFromDetails,
  profileUpdateBody,
  validateProfileDraft,
  type CustomerProfile,
  type ProfileFieldErrors,
} from "@/lib/profile";
import { TAX_ID_TYPES, TAX_ID_TYPE_LABELS, isTaxIdType, type TaxIdType } from "@/lib/tax-id";

/**
 * "My info" in the Customer Area (#102): what the platform holds about the
 * signed-in Customer, and the one place they can change it without starting a
 * checkout.
 *
 * The email is shown and not editable. It is the Customer's identity — every
 * Ticket Sale they own hangs off it — so changing it is not a form field but a
 * different feature, and saying so plainly beats offering a disabled input with
 * no explanation.
 *
 * Editing changes what the person asserts *now*. Past purchases keep the name
 * and Tax ID they were transacted under, which is why the panel says so rather
 * than leaving someone to wonder whether fixing a typo rewrites their receipts.
 */

type MyInfoProps = { profile: CustomerProfile };

/** Matches the Input component's height and border so the select reads as a peer. */
const SELECT_CLASS =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

/** The stored Tax ID as a line of prose, or the em dash history uses for "none". */
function taxIdLabel(profile: CustomerProfile): string {
  if (!profile.tax_id_type || !profile.tax_id_number) return "—";
  const label = isTaxIdType(profile.tax_id_type)
    ? TAX_ID_TYPE_LABELS[profile.tax_id_type]
    : profile.tax_id_type;
  return `${label}: ${profile.tax_id_number}`;
}

function fullName(profile: CustomerProfile): string {
  const name = `${profile.first_name} ${profile.last_name}`.trim();
  return name === "" ? "—" : name;
}

export function MyInfo({ profile: initialProfile }: MyInfoProps) {
  const router = useRouter();
  // The saved profile is held here as well as on the server so the panel shows
  // the new values the instant the API confirms them, without waiting for the
  // page to be re-rendered.
  const [profile, setProfile] = useState(initialProfile);
  const [editing, setEditing] = useState(false);
  const [firstName, setFirstName] = useState(initialProfile.first_name);
  const [lastName, setLastName] = useState(initialProfile.last_name);
  const [taxIdType, setTaxIdType] = useState<TaxIdType>(
    initialProfile.tax_id_type && isTaxIdType(initialProfile.tax_id_type)
      ? initialProfile.tax_id_type
      : "cedula",
  );
  const [taxIdNumber, setTaxIdNumber] = useState(initialProfile.tax_id_number ?? "");
  const [fieldErrors, setFieldErrors] = useState<ProfileFieldErrors>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  /** Opening the form starts from what is stored, discarding any abandoned edit. */
  function startEditing() {
    setFirstName(profile.first_name);
    setLastName(profile.last_name);
    setTaxIdType(
      profile.tax_id_type && isTaxIdType(profile.tax_id_type) ? profile.tax_id_type : "cedula",
    );
    setTaxIdNumber(profile.tax_id_number ?? "");
    setFieldErrors({});
    setError(null);
    setEditing(true);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const draft = { firstName, lastName, taxIdType, taxIdNumber };

    // The mirror check: a blank name or a mistyped cédula is caught here so the
    // Customer is told before a round trip. The API applies the same rules and
    // its verdict is the one that decides whether the edit lands.
    const problems = validateProfileDraft(draft);
    if (Object.keys(problems).length > 0) {
      setError(null);
      setFieldErrors(problems);
      return;
    }

    setSaving(true);
    setError(null);
    setFieldErrors({});

    try {
      const response = await fetch("/api/customer/profile", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(profileUpdateBody(draft)),
      });
      const envelope = (await response.json()) as Envelope<CustomerProfile>;
      if (!response.ok || envelope.error || !envelope.data) {
        setFieldErrors(profileFieldErrorsFromDetails(envelope.error?.details));
        setError(envelope.error?.message ?? "Your details could not be saved. Please try again.");
        setSaving(false);
        return;
      }
      setProfile(envelope.data);
      setEditing(false);
      setSaving(false);
      // Transient success, the way every other save in the system reports one
      // (docs/design/foundation.md): the panel already shows the new values, so
      // nothing persistent needs to sit on the page.
      toast.success("Your details are saved.");
      // The server-rendered page holds the same session payload this panel came
      // from; refreshing keeps the two from drifting apart on a soft navigation.
      router.refresh();
    } catch {
      setError("Your details could not be saved. Please check your connection and try again.");
      setSaving(false);
    }
  }

  return (
    <section className="space-y-4" aria-labelledby="my-info-heading">
      <div className="flex items-center justify-between gap-4">
        <h2 id="my-info-heading" className="text-lg font-semibold tracking-tight">
          My info
        </h2>
        {editing ? null : (
          <Button type="button" variant="secondary" size="sm" onClick={startEditing}>
            Edit
          </Button>
        )}
      </div>

      <div className="rounded-lg border bg-card p-5 sm:p-6">
        <dl className="space-y-3 text-sm">
          <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">Email</dt>
            <dd className="font-medium">{profile.email}</dd>
          </div>
          {editing ? null : (
            <>
              <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">Name</dt>
                <dd className="font-medium">{fullName(profile)}</dd>
              </div>
              <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">ID</dt>
                <dd className="font-medium">{taxIdLabel(profile)}</dd>
              </div>
            </>
          )}
        </dl>

        {editing ? (
          <form className="mt-4 space-y-4 border-t pt-4" onSubmit={handleSubmit} noValidate>
            {error ? (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="my-info-first-name" label="First name" error={fieldErrors.first_name}>
                <Input
                  name="given-name"
                  autoComplete="given-name"
                  required
                  value={firstName}
                  onChange={(event) => setFirstName(event.target.value)}
                />
              </FormField>
              <FormField id="my-info-last-name" label="Last name" error={fieldErrors.last_name}>
                <Input
                  name="family-name"
                  autoComplete="family-name"
                  required
                  value={lastName}
                  onChange={(event) => setLastName(event.target.value)}
                />
              </FormField>
            </div>

            {/* Type and number are one fact split across two controls, so they
                sit on one row — and they clear together: emptying the number
                removes the stored ID entirely. */}
            <div className="grid gap-4 sm:grid-cols-[minmax(0,9rem)_1fr]">
              <FormField id="my-info-tax-id-type" label="ID type" error={fieldErrors.tax_id_type}>
                <select
                  name="tax-id-type"
                  className={SELECT_CLASS}
                  value={taxIdType}
                  onChange={(event) => {
                    if (isTaxIdType(event.target.value)) setTaxIdType(event.target.value);
                  }}
                >
                  {TAX_ID_TYPES.map((type) => (
                    <option key={type} value={type}>
                      {TAX_ID_TYPE_LABELS[type]}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField
                id="my-info-tax-id-number"
                label="ID number"
                error={fieldErrors.tax_id_number}
                description="Leave blank to remove it. We'll ask again at your next checkout."
              >
                <Input
                  name="tax-id-number"
                  inputMode={taxIdType === "passport" ? "text" : "numeric"}
                  autoComplete="off"
                  value={taxIdNumber}
                  onChange={(event) => setTaxIdNumber(event.target.value)}
                />
              </FormField>
            </div>

            <p className="text-sm text-muted-foreground">
              Your email is how we find your tickets, so it can&apos;t be changed here. Past
              purchases keep the details they were bought with.
            </p>

            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={saving} aria-busy={saving}>
                {saving ? "Saving…" : "Save changes"}
              </Button>
              <Button
                type="button"
                variant="ghost"
                disabled={saving}
                onClick={() => setEditing(false)}
              >
                Cancel
              </Button>
            </div>
          </form>
        ) : null}
      </div>
    </section>
  );
}
