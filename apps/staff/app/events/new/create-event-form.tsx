"use client";

import { useMessages, useTranslations } from "next-intl";
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

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError, fetchEventsJSON, slugify, type EventDetail } from "@/lib/events-api";

export function CreateEventForm() {
  const t = useTranslations("events");
  const errorCopy = useMessages().errors;
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
      toast.success(t("createdToast"));
      router.push(`/events/${created.id}`);
      router.refresh();
    } catch (submitError) {
      toast.error(
        apiErrorMessage(errorCopy, submitError instanceof ApiError ? submitError : null) ??
          t("createFailed"),
      );
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("newTitle")} description={t("newDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("basicsTitle")}</CardTitle>
          <CardDescription>{t("basicsDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(event) => void handleSubmit(event)}>
            <FormField id="event-name" label={t("nameLabel")}>
              <Input value={name} onChange={(event) => handleNameChange(event.target.value)} required />
            </FormField>
            <FormField id="event-slug" label={t("slugLabel")} description={t("slugDescription")}>
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
              {loading ? t("creating") : t("createEvent")}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
