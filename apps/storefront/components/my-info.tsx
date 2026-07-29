"use client";

import { Alert, AlertDescription, Button, FormField, Input, toast } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useMemo, useRef, useState, type ChangeEvent, type FormEvent } from "react";

import { CustomerAvatar } from "@/components/customer-avatar";
import { apiErrorMessage } from "@/lib/api-errors";
import { intlLocale, toAppLocale } from "@/lib/locale";
import { countries } from "@/lib/phone";
import {
  profileDraftPhone,
  profileFieldErrorsFromDetails,
  profileFieldMessages,
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

/**
 * The image types the API's allowlist accepts, mirrored here so a .gif is
 * refused before any round trip. The server's verdict still decides.
 */
const AVATAR_CONTENT_TYPES = ["image/jpeg", "image/png", "image/webp"];

/** Mirrors the seeding cap on the API; a phone photo fits comfortably. */
const MAX_AVATAR_BYTES = 5 * 1024 * 1024;

/**
 * The cap as the two sentences about it state it. Interpolated rather than
 * written into the catalogs, so raising the limit does not leave "5 MB" behind
 * in a language nobody on this team reads.
 */
const MAX_AVATAR_MEGABYTES = MAX_AVATAR_BYTES / (1024 * 1024);

/** The stored Tax ID as a line of prose, or the em dash history uses for "none". */
function taxIdLabel(profile: CustomerProfile): string {
  if (!profile.tax_id_type || !profile.tax_id_number) return "—";
  const label = isTaxIdType(profile.tax_id_type)
    ? TAX_ID_TYPE_LABELS[profile.tax_id_type]
    : profile.tax_id_type;
  return `${label}: ${profile.tax_id_number}`;
}

/**
 * The stored phone as it is held — canonical E.164, plus and all — or the em
 * dash for "none".
 *
 * Shown canonical rather than split back into "Ecuador +593 / 98 765 4321",
 * because this line answers "what does the platform hold about me", and the
 * answer is one string. The two-control split belongs to the form below, where
 * it is how a number gets typed.
 */
function phoneLabel(profile: CustomerProfile): string {
  return profile.phone ?? "—";
}

function fullName(profile: CustomerProfile): string {
  const name = `${profile.first_name} ${profile.last_name}`.trim();
  return name === "" ? "—" : name;
}

export function MyInfo({ profile: initialProfile }: MyInfoProps) {
  const router = useRouter();
  const t = useTranslations("myInfo");
  /**
   * The error catalog as plain data, and "myInfo" as the surface every call
   * below passes.
   *
   * The surface earns its keep on one code: all four writes here —
   * the profile PATCH and the three Avatar hops — are refused to a Confirmation
   * Link session with CUSTOMER_SESSION_SCOPE_INSUFFICIENT, which the undo dialog
   * also receives and which means something different there. The code alone
   * cannot tell them apart; the surface can (ADR 0022).
   */
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
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
  // The stored number resolved back into the two controls a person edits it
  // with. A Customer who has none gets Ecuador and an empty field, which is the
  // same state the checkout dialog opens in.
  const initialPhone = profileDraftPhone(initialProfile.phone);
  const [phoneDiallingCode, setPhoneDiallingCode] = useState(initialPhone.phoneDiallingCode);
  const [phoneNationalNumber, setPhoneNationalNumber] = useState(initialPhone.phoneNationalNumber);
  const [fieldErrors, setFieldErrors] = useState<ProfileFieldErrors>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [photoBusy, setPhotoBusy] = useState(false);
  const [photoError, setPhotoError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Two hundred region names and a collation sort, held across the keystrokes
  // that re-render the form around the selector.
  const countryRows = useMemo(() => countries(intlLocale(locale)), [locale]);

  /**
   * The photo flow is three hops the person sees as one: mint a presigned URL,
   * PUT the file straight to object storage, then attach the key. The profile
   * the attach returns is the source of truth for what now shows.
   */
  async function handlePhotoChosen(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    // Reset so choosing the same file again still fires a change event.
    event.target.value = "";
    if (!file) return;

    // Both checks run before any request, so what they say is this app's own
    // copy rather than a relayed API message.
    if (!AVATAR_CONTENT_TYPES.includes(file.type)) {
      setPhotoError(t("photoTypeRefused"));
      return;
    }
    if (file.size > MAX_AVATAR_BYTES) {
      setPhotoError(t("photoTooLarge", { limit: MAX_AVATAR_MEGABYTES }));
      return;
    }

    setPhotoBusy(true);
    setPhotoError(null);
    try {
      const ticketResponse = await fetch("/api/customer/profile/avatar-upload-url", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content_type: file.type, file_name: file.name }),
      });
      const ticket = (await ticketResponse.json()) as Envelope<{
        upload_url: string;
        object_key: string;
      }>;
      if (!ticketResponse.ok || ticket.error || !ticket.data) {
        setPhotoError(
          apiErrorMessage(errorCopy, ticket.error, "myInfo") ?? t("photoUploadFailed"),
        );
        return;
      }

      const upload = await fetch(ticket.data.upload_url, {
        method: "PUT",
        headers: { "Content-Type": file.type },
        body: file,
      });
      if (!upload.ok) {
        // Object storage refused the PUT. No envelope came back from it — this
        // hop never touches the API — so there is nothing to key copy on.
        setPhotoError(t("photoUploadFailed"));
        return;
      }

      const attachResponse = await fetch("/api/customer/profile/avatar", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ image_key: ticket.data.object_key }),
      });
      const attached = (await attachResponse.json()) as Envelope<CustomerProfile>;
      if (!attachResponse.ok || attached.error || !attached.data) {
        setPhotoError(apiErrorMessage(errorCopy, attached.error, "myInfo") ?? t("photoSaveFailed"));
        return;
      }

      setProfile(attached.data);
      toast.success(t("photoSaved"));
      // The header chip renders from the server-side session read; refreshing
      // keeps it in step with the photo this panel now shows.
      router.refresh();
    } catch {
      setPhotoError(t("photoUploadNetworkFailed"));
    } finally {
      setPhotoBusy(false);
    }
  }

  async function handlePhotoRemove() {
    setPhotoBusy(true);
    setPhotoError(null);
    try {
      const response = await fetch("/api/customer/profile/avatar", { method: "DELETE" });
      const envelope = (await response.json()) as Envelope<CustomerProfile>;
      if (!response.ok || envelope.error || !envelope.data) {
        setPhotoError(
          apiErrorMessage(errorCopy, envelope.error, "myInfo") ?? t("photoRemoveFailed"),
        );
        return;
      }
      setProfile(envelope.data);
      toast.success(t("photoRemoved"));
      router.refresh();
    } catch {
      setPhotoError(t("photoRemoveNetworkFailed"));
    } finally {
      setPhotoBusy(false);
    }
  }

  /** Opening the form starts from what is stored, discarding any abandoned edit. */
  function startEditing() {
    setFirstName(profile.first_name);
    setLastName(profile.last_name);
    setTaxIdType(
      profile.tax_id_type && isTaxIdType(profile.tax_id_type) ? profile.tax_id_type : "cedula",
    );
    setTaxIdNumber(profile.tax_id_number ?? "");
    const phone = profileDraftPhone(profile.phone);
    setPhoneDiallingCode(phone.phoneDiallingCode);
    setPhoneNationalNumber(phone.phoneNationalNumber);
    setFieldErrors({});
    setError(null);
    setEditing(true);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const draft = {
      firstName,
      lastName,
      taxIdType,
      taxIdNumber,
      phoneDiallingCode,
      phoneNationalNumber,
    };

    // The mirror check: a blank name or a mistyped cédula is caught here so the
    // Customer is told before a round trip. The API applies the same rules and
    // its verdict is the one that decides whether the edit lands.
    //
    // It answers with codes, and they are resolved through the same catalog
    // entries the API's own field errors resolve through, so a Customer who
    // mistypes their cédula reads one sentence rather than an English one now
    // and a Spanish one after the round trip (ADR 0022).
    const problems = validateProfileDraft(draft);
    if (Object.keys(problems).length > 0) {
      setError(null);
      setFieldErrors(profileFieldMessages(errorCopy, problems));
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
        setFieldErrors(profileFieldErrorsFromDetails(errorCopy, envelope.error?.details));
        setError(apiErrorMessage(errorCopy, envelope.error, "myInfo") ?? t("saveFailed"));
        setSaving(false);
        return;
      }
      setProfile(envelope.data);
      setEditing(false);
      setSaving(false);
      // Transient success, the way every other save in the system reports one
      // (docs/design/foundation.md): the panel already shows the new values, so
      // nothing persistent needs to sit on the page.
      toast.success(t("saved"));
      // The server-rendered page holds the same session payload this panel came
      // from; refreshing keeps the two from drifting apart on a soft navigation.
      router.refresh();
    } catch {
      setError(t("saveNetworkFailed"));
      setSaving(false);
    }
  }

  return (
    // scroll-mt keeps the heading clear of the sticky header when the header
    // menu's "My info" link lands on this anchor.
    <section id="my-info" className="scroll-mt-20 space-y-4" aria-labelledby="my-info-heading">
      <div className="flex items-center justify-between gap-4">
        <h2 id="my-info-heading" className="text-lg font-semibold tracking-tight">
          {t("heading")}
        </h2>
        {editing ? null : (
          <Button type="button" variant="secondary" size="sm" onClick={startEditing}>
            {t("edit")}
          </Button>
        )}
      </div>

      <div className="rounded-lg border bg-card p-5 sm:p-6">
        {/* The Avatar row: photo or initials, and the two things a person can do
            about it. Separate from the edit form because it saves on its own —
            choosing a photo needs no Save button, and removing one is not an
            edit in progress. */}
        <div className="mb-4 flex items-center gap-4 border-b pb-4">
          <CustomerAvatar
            avatarUrl={profile.avatar_url}
            firstName={profile.first_name}
            lastName={profile.last_name}
            email={profile.email}
            className="h-16 w-16 text-lg"
          />
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={photoBusy}
                aria-busy={photoBusy}
                onClick={() => fileInputRef.current?.click()}
              >
                {photoBusy
                  ? t("photoWorking")
                  : profile.avatar_url
                    ? t("changePhoto")
                    : t("addPhoto")}
              </Button>
              {profile.avatar_url ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={photoBusy}
                  onClick={handlePhotoRemove}
                >
                  {t("removePhoto")}
                </Button>
              ) : null}
            </div>
            <p className="text-xs text-muted-foreground">
              {t("photoHint", { limit: MAX_AVATAR_MEGABYTES })}
            </p>
            {photoError ? <p className="text-xs text-destructive">{photoError}</p> : null}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept={AVATAR_CONTENT_TYPES.join(",")}
            className="hidden"
            onChange={handlePhotoChosen}
          />
        </div>

        <dl className="space-y-3 text-sm">
          <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">{t("emailLabel")}</dt>
            <dd className="font-medium">{profile.email}</dd>
          </div>
          {editing ? null : (
            <>
              <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">{t("nameLabel")}</dt>
                <dd className="font-medium">{fullName(profile)}</dd>
              </div>
              <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">{t("idLabel")}</dt>
                {/* "Cédula", "RUC" and "Pasaporte" name Ecuadorian documents and
                    read the same in both languages, so the value beside this
                    term is not in the catalog. */}
                <dd className="font-medium">{taxIdLabel(profile)}</dd>
              </div>
              {/* The phone sits beside the ID because they are the same kind of
                  thing to the person reading: details the platform holds, given
                  once, reused at every checkout (#108). */}
              <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">{t("phoneLabel")}</dt>
                <dd className="font-medium tabular-nums">{phoneLabel(profile)}</dd>
              </div>
            </>
          )}
        </dl>

        {/* The completeness nudge: checkout requires a Tax ID, so a Customer
            who stores one here skips typing it at every purchase. Shown only
            while one is missing and no edit is underway — a nudge under the
            very form that answers it would be noise. */}
        {!editing && !profile.tax_id_type ? (
          <Alert className="mt-4">
            <AlertDescription>{t("idNudge")}</AlertDescription>
          </Alert>
        ) : null}

        {editing ? (
          <form className="mt-4 space-y-4 border-t pt-4" onSubmit={handleSubmit} noValidate>
            {error ? (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                id="my-info-first-name"
                label={t("firstNameLabel")}
                error={fieldErrors.first_name}
              >
                <Input
                  name="given-name"
                  autoComplete="given-name"
                  required
                  value={firstName}
                  onChange={(event) => setFirstName(event.target.value)}
                />
              </FormField>
              <FormField
                id="my-info-last-name"
                label={t("lastNameLabel")}
                error={fieldErrors.last_name}
              >
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
              <FormField
                id="my-info-tax-id-type"
                label={t("taxIdTypeLabel")}
                error={fieldErrors.tax_id_type}
              >
                <select
                  name="tax-id-type"
                  className={SELECT_CLASS}
                  value={taxIdType}
                  onChange={(event) => {
                    if (isTaxIdType(event.target.value)) setTaxIdType(event.target.value);
                  }}
                >
                  {/* Cédula, RUC and Pasaporte are the names of Ecuadorian
                      documents, so they read the same in both languages and are
                      not in the catalog. */}
                  {TAX_ID_TYPES.map((type) => (
                    <option key={type} value={type}>
                      {TAX_ID_TYPE_LABELS[type]}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField
                id="my-info-tax-id-number"
                label={t("taxIdNumberLabel")}
                error={fieldErrors.tax_id_number}
                description={t("taxIdNumberHint")}
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

            {/* The phone, entered the way the checkout dialog enters it — a
                country and a national number, assembled into the one canonical
                string that is stored and sent. Optional here as it is there
                (#103): a Customer who never gives one loses nothing, and one
                who empties the field withdraws the number entirely. */}
            <div className="grid gap-4 sm:grid-cols-[minmax(0,11rem)_1fr]">
              <FormField id="my-info-phone-country" label={t("phoneCountryLabel")}>
                <select
                  name="phone-country"
                  className={SELECT_CLASS}
                  value={phoneDiallingCode}
                  onChange={(event) => setPhoneDiallingCode(event.target.value)}
                >
                  {/* Keyed by region code, valued by dialling code: the region
                      is the row's identity and its name is only a rendering of
                      it, so the key survives a change of language. The dialling
                      codes are not unique (+1 covers the US, Canada and twenty
                      more), so a stored +1 number shows under the first country
                      listed under it. The accepted cosmetic imperfection from
                      #103 — the number stored and sent is identical either
                      way. */}
                  {countryRows.map((country) => (
                    <option key={country.regionCode} value={country.diallingCode}>
                      {t("countryOption", {
                        country: country.name,
                        diallingCode: country.diallingCode,
                      })}
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField
                id="my-info-phone"
                label={t("phoneFieldLabel")}
                error={fieldErrors.phone}
                description={t("phoneHint")}
              >
                <Input
                  name="tel-national"
                  type="tel"
                  inputMode="tel"
                  autoComplete="tel-national"
                  value={phoneNationalNumber}
                  onChange={(event) => setPhoneNationalNumber(event.target.value)}
                />
              </FormField>
            </div>

            <p className="text-sm text-muted-foreground">{t("emailNote")}</p>

            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={saving} aria-busy={saving}>
                {saving ? t("saving") : t("save")}
              </Button>
              <Button
                type="button"
                variant="ghost"
                disabled={saving}
                onClick={() => setEditing(false)}
              >
                {t("cancel")}
              </Button>
            </div>
          </form>
        ) : null}
      </div>
    </section>
  );
}
