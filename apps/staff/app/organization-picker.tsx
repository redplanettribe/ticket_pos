"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Skeleton,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { MembershipList, type Membership } from "@/app/membership-list";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

type OrganizationPickerProps = {
  title: string;
  description: string;
  redirectTo?: string;
  footer?: ReactNode;
  actions?: ReactNode;
};

export function OrganizationPicker({
  title,
  description,
  redirectTo = "/",
  footer,
  actions,
}: OrganizationPickerProps) {
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
    return (
      <AuthCard title={title} description={description} footer={footer}>
        <div className="space-y-3">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      </AuthCard>
    );
  }

  return (
    <AuthCard title={title} description={description} footer={footer}>
      {actions}
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <MembershipList
        memberships={memberships}
        onSelect={handleSelect}
        selecting={selecting}
      />
    </AuthCard>
  );
}
