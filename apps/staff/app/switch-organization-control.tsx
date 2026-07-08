"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

type Membership = {
  member_id: string;
  organization_name: string;
  organization_slug: string;
  role: string;
};

type SwitchOrganizationControlProps = {
  memberships: Membership[];
  activeMemberId: string;
};

export function SwitchOrganizationControl({ memberships, activeMemberId }: SwitchOrganizationControlProps) {
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (memberships.length <= 1) {
    return null;
  }

  async function handleChange(memberId: string) {
    if (memberId === activeMemberId) {
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/auth/select-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ member_id: memberId }),
      });
      const envelope = (await response.json()) as {
        error: { message: string } | null;
      };
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not switch organization");
        return;
      }
      router.refresh();
    } catch {
      setError("Could not switch organization");
    } finally {
      setLoading(false);
    }
  }

  return (
    <label>
      Switch organization{" "}
      <select
        value={activeMemberId}
        disabled={loading}
        onChange={(event) => void handleChange(event.target.value)}
      >
        {memberships.map((membership) => (
          <option key={membership.member_id} value={membership.member_id}>
            {membership.organization_name}
          </option>
        ))}
      </select>
      {error ? <p role="alert">{error}</p> : null}
    </label>
  );
}
