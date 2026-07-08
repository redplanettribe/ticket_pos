"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

type Membership = {
  member_id: string;
  organization_id: string;
  organization_name: string;
  organization_slug: string;
  role: string;
};

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

type OrganizationPickerProps = {
  title: string;
  description: string;
  redirectTo?: string;
};

export function OrganizationPicker({ title, description, redirectTo = "/" }: OrganizationPickerProps) {
  const router = useRouter();
  const [memberships, setMemberships] = useState<Membership[]>([]);
  const [loading, setLoading] = useState(true);
  const [selecting, setSelecting] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function loadMemberships() {
      try {
        const response = await fetch("/api/auth/memberships");
        const envelope = (await response.json()) as Envelope<Membership[]>;
        if (!response.ok || envelope.error) {
          setError(envelope.error?.message ?? "Could not load organizations");
          return;
        }
        setMemberships(envelope.data ?? []);
      } catch {
        setError("Could not load organizations");
      } finally {
        setLoading(false);
      }
    }

    void loadMemberships();
  }, []);

  async function handleSelect(memberId: string) {
    setSelecting(memberId);
    setError(null);

    try {
      const response = await fetch("/api/auth/select-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ member_id: memberId }),
      });
      const envelope = (await response.json()) as Envelope<{ active_member: { member_id: string } | null }>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not select organization");
        return;
      }
      router.push(redirectTo);
      router.refresh();
    } catch {
      setError("Could not select organization");
    } finally {
      setSelecting(null);
    }
  }

  if (loading) {
    return <p>Loading organizations...</p>;
  }

  return (
    <section>
      <h1>{title}</h1>
      <p>{description}</p>
      {memberships.length === 0 ? <p>No organizations found for your account.</p> : null}
      <ul>
        {memberships.map((membership) => (
          <li key={membership.member_id}>
            <button
              type="button"
              onClick={() => handleSelect(membership.member_id)}
              disabled={selecting !== null}
            >
              {selecting === membership.member_id ? "Selecting..." : membership.organization_name}
            </button>
            <span>
              {" "}
              ({membership.organization_slug} · {membership.role.replace("_", " ")})
            </span>
          </li>
        ))}
      </ul>
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
