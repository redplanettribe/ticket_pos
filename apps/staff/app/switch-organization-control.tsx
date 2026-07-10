"use client";

import { Alert, AlertDescription, FormField } from "@ticket-pos/ui";
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
    <div className="space-y-2">
      <FormField id="switch-organization" label="Switch organization">
        <select
          id="switch-organization"
          className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
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
      </FormField>
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}
