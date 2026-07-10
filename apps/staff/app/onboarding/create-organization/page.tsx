"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Button,
  FormField,
  Input,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { LogoutButton } from "@/app/logout-button";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

function slugify(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export default function CreateOrganizationPage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [error, setError] = useState<string | null>(null);
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

    try {
      const response = await fetch("/api/auth/create-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, slug }),
      });
      const envelope = (await response.json()) as Envelope<{ active_member: { member_id: string } | null }>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not create organization");
        return;
      }
      router.push("/");
      router.refresh();
    } catch {
      setError("Could not create organization");
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthCard
      title="Create your organization"
      description="Name your venue and choose a URL slug for your storefront."
      footer={<LogoutButton />}
    >
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <form className="space-y-4" onSubmit={handleSubmit}>
        <FormField id="name" label="Organization name">
          <Input
            id="name"
            name="name"
            type="text"
            required
            value={name}
            onChange={(event) => handleNameChange(event.target.value)}
          />
        </FormField>
        <FormField id="slug" label="Slug" description="Used in your storefront URL.">
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
          {loading ? "Creating..." : "Create organization"}
        </Button>
      </form>
    </AuthCard>
  );
}
