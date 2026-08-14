"use client";

import { useMessages, useTranslations } from "next-intl";
import { useState } from "react";

import { Button, toast } from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, setEventDiscoverable } from "@/lib/events-api";

type DiscoverabilityToggleProps = {
  eventId: string;
  status: string;
  discoverable: boolean;
  onChange: (discoverable: boolean) => void;
};

export function DiscoverabilityToggle({ eventId, status, discoverable, onChange }: DiscoverabilityToggleProps) {
  const t = useTranslations("events");
  const errorCopy = useMessages().errors;
  const [saving, setSaving] = useState(false);
  const published = status === "published";

  async function toggle() {
    if (saving) {
      return;
    }
    const next = !discoverable;
    setSaving(true);
    try {
      const updated = await setEventDiscoverable(eventId, next);
      onChange(updated.discoverable);
      toast.success(next ? t("nowListedToast") : t("noLongerListedToast"));
    } catch (error) {
      toast.error(
        apiErrorMessage(errorCopy, error instanceof ApiError ? error : null) ?? t("listingFailed"),
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Button
      type="button"
      variant={discoverable ? "secondary" : "outline"}
      size="sm"
      disabled={!published || saving}
      aria-pressed={discoverable}
      title={published ? undefined : t("publishToList")}
      onClick={(event) => {
        event.preventDefault();
        void toggle();
      }}
    >
      {discoverable ? t("listed") : t("notListed")}
    </Button>
  );
}
