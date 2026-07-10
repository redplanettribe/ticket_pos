"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  PageHeader,
  toast,
} from "@ticket-pos/ui";

import { fetchEventsJSON, slugify, type EventDetail } from "@/lib/events-api";

export function CreateEventForm() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
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

    try {
      const created = await fetchEventsJSON<EventDetail>("/api/events", {
        method: "POST",
        body: JSON.stringify({ name, slug }),
      });
      toast.success("Event created");
      router.push(`/events/${created.id}`);
      router.refresh();
    } catch (submitError) {
      toast.error(submitError instanceof Error ? submitError.message : "Failed to create event");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Create event"
        description="Start with a name and slug. You can add scheduling and details on the next screen."
      />

      <Card>
        <CardHeader>
          <CardTitle>Event basics</CardTitle>
          <CardDescription>Draft events can be edited freely until you publish.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(event) => void handleSubmit(event)}>
            <FormField id="event-name" label="Name">
              <Input value={name} onChange={(event) => handleNameChange(event.target.value)} required />
            </FormField>
            <FormField
              id="event-slug"
              label="Slug"
              description="Used in Storefront URLs. Lowercase letters, numbers, and hyphens only."
            >
              <Input
                value={slug}
                onChange={(event) => {
                  setSlugEdited(true);
                  setSlug(event.target.value);
                }}
                required
              />
            </FormField>
            <Button type="submit" disabled={loading}>
              {loading ? "Creating..." : "Create event"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
