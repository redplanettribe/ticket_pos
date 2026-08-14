"use client";

import {
  Alert,
  AlertDescription,
  Button,
  FormField,
  Input,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

/**
 * The suggested slug, derived from the name as it is typed.
 *
 * Deliberately ASCII-only and language-blind: a slug is part of a URL, not copy,
 * and the same rule runs whichever language the form is being read in. An
 * accented name loses its accents here rather than in the address bar, and the
 * organizer can always overwrite the suggestion.
 */
function slugify(name: string): string {
  return name
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function CreateOrganizationForm() {
  const t = useTranslations("onboarding");
  const errorCopy = useMessages().errors;
  const router = useRouter();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);

  function handleNameChange(value: string) {
    setName(value);
    if (!slugEdited) {
      setSlug(slugify(value));
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    setFieldErrors({});

    try {
      const response = await fetch("/api/auth/create-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, slug }),
      });
      const envelope = (await response.json()) as Envelope<{
        active_member: { member_id: string } | null;
      }>;
      if (!response.ok || envelope.error) {
        // The API's code first — ORGANIZATION_SLUG_TAKEN is the refusal this form
        // provokes most — its English message as the floor, and this surface's
        // own sentence only when nothing reached the API (ADR 0023).
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("createFailed"));
        setFieldErrors(fieldErrorMessages(errorCopy, envelope.error?.details));
        return;
      }
      router.push("/");
      router.refresh();
    } catch {
      setError(t("createFailed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <>
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <form className="space-y-4" onSubmit={handleSubmit}>
        <FormField id="name" label={t("nameLabel")} error={fieldErrors.name}>
          <Input
            id="name"
            name="name"
            type="text"
            required
            value={name}
            onChange={(event) => handleNameChange(event.target.value)}
          />
        </FormField>
        <FormField
          id="slug"
          label={t("slugLabel")}
          description={t("slugDescription")}
          error={fieldErrors.slug}
        >
          <Input
            id="slug"
            name="slug"
            type="text"
            required
            value={slug}
            onChange={(event) => {
              setSlugEdited(true);
              setSlug(event.target.value);
            }}
          />
        </FormField>
        <Button type="submit" className="w-full" disabled={loading} aria-busy={loading}>
          {loading ? t("creating") : t("createSubmit")}
        </Button>
      </form>
    </>
  );
}
