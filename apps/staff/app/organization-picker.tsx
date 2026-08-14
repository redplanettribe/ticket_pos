"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Skeleton,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { MembershipList, type Membership } from "@/app/membership-list";
import { apiErrorMessage } from "@/lib/api-errors";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
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
  const t = useTranslations("shell");
  const errorCopy = useMessages().errors;
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
          setError(apiErrorMessage(errorCopy, envelope.error) ?? t("loadOrganizationsFailed"));
          return;
        }
        setMemberships(envelope.data ?? []);
      } catch {
        setError(t("loadOrganizationsFailed"));
      } finally {
        setLoading(false);
      }
    }

    void loadMemberships();
    // Runs once, on mount. `t` and `errorCopy` are stable for the life of a
    // render tree — the locale cannot change without a server round trip, which
    // remounts this component anyway.
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("selectOrganizationFailed"));
        return;
      }
      router.push(redirectTo);
      router.refresh();
    } catch {
      setError(t("selectOrganizationFailed"));
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
