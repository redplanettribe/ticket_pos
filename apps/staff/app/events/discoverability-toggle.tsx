"use client";

import { useState } from "react";

import { Button, toast } from "@ticket-pos/ui";

import { ApiError, setEventDiscoverable } from "@/lib/events-api";

type DiscoverabilityToggleProps = {
  eventId: string;
  status: string;
  discoverable: boolean;
  onChange: (discoverable: boolean) => void;
};

export function DiscoverabilityToggle({ eventId, status, discoverable, onChange }: DiscoverabilityToggleProps) {
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
      toast.success(next ? "Event is now listed publicly." : "Event is no longer listed.");
    } catch (error) {
      const message = error instanceof ApiError ? error.message : "Could not update discoverability";
      toast.error(message);
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
      title={published ? undefined : "Publish the event to make it discoverable."}
      onClick={(event) => {
        event.preventDefault();
        void toggle();
      }}
    >
      {discoverable ? "Listed" : "Not listed"}
    </Button>
  );
}
