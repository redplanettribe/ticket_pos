"use client";

import { useMessages, useTranslations } from "next-intl";
import { ChangeEvent, useRef, useState } from "react";

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  toast,
} from "@ticket-pos/ui";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";

const MAX_LOGO_BYTES = 5 * 1024 * 1024;
const ACCEPTED_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);
const MIN_LOGO_DIMENSION = 64;

/**
 * What the Logo must be, as numbers rather than as a sentence.
 *
 * Handed to the catalog as ICU values so the description and each refusal name
 * the same limits the code enforces, in whichever language, and so a limit that
 * changes changes in one place instead of in two sentences per language.
 */
const LOGO_LIMITS = {
  maxMegabytes: MAX_LOGO_BYTES / 1024 / 1024,
  minPixels: MIN_LOGO_DIMENSION,
} as const;

/** Resolves the image's pixel dimensions by loading it off-DOM. */
function readImageDimensions(file: File): Promise<{ width: number; height: number }> {
  return new Promise((resolve, reject) => {
    const objectUrl = URL.createObjectURL(file);
    const image = new Image();
    image.onload = () => {
      URL.revokeObjectURL(objectUrl);
      resolve({ width: image.naturalWidth, height: image.naturalHeight });
    };
    image.onerror = () => {
      URL.revokeObjectURL(objectUrl);
      reject(new Error("unreadable"));
    };
    image.src = objectUrl;
  });
}

type LogoUploadURL = {
  upload_url: string;
  object_key: string;
  public_url: string;
};

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

/**
 * Fails as an `ApiError` carrying the API's code, so a refusal can be shown in
 * the reader's language rather than only in the API's English (ADR 0023).
 */
async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new ApiError(
      envelope.error?.message ?? "",
      envelope.error?.code,
      envelope.error?.details as Record<string, unknown> | undefined,
    );
  }
  if (envelope.data === null) {
    throw new ApiError("");
  }
  return envelope.data;
}

type OrgLogoImageProps = {
  organizationName: string;
  logoUrl: string | null;
  onUpdated: (logoUrl: string | null) => void;
};

export function OrgLogoImage({ organizationName, logoUrl, onUpdated }: OrgLogoImageProps) {
  const t = useTranslations("organization");
  const errorCopy = useMessages().errors;
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [removing, setRemoving] = useState(false);

  async function attachLogo(objectKey: string) {
    const updated = await fetchJSON<{ logo_url: string | null }>("/api/settings/organization", {
      method: "PATCH",
      body: JSON.stringify({ name: organizationName, logo_image_key: objectKey }),
    });
    onUpdated(updated.logo_url);
  }

  async function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    if (!ACCEPTED_TYPES.has(file.type)) {
      toast.error(t("logoTypeInvalid"));
      return;
    }
    if (file.size > MAX_LOGO_BYTES) {
      toast.error(t("logoTooLarge", LOGO_LIMITS));
      return;
    }

    try {
      const { width, height } = await readImageDimensions(file);
      if (Math.min(width, height) < MIN_LOGO_DIMENSION) {
        toast.error(t("logoTooSmall", LOGO_LIMITS));
        return;
      }
    } catch {
      toast.error(t("logoUnreadable"));
      return;
    }

    setUploading(true);
    try {
      const presign = await fetchJSON<LogoUploadURL>("/api/settings/organization/logo-upload-url", {
        method: "POST",
        body: JSON.stringify({ content_type: file.type, file_name: file.name }),
      });

      const uploadResponse = await fetch(presign.upload_url, {
        method: "PUT",
        headers: { "Content-Type": file.type },
        body: file,
      });
      if (!uploadResponse.ok) {
        // Object storage refused, not the API: there is no envelope and no code
        // to key a sentence on, so this surface says it in its own words.
        throw new ApiError("");
      }

      await attachLogo(presign.object_key);
      toast.success(t("logoUploadedToast"));
    } catch (uploadError) {
      toast.error(
        apiErrorMessage(errorCopy, uploadError instanceof ApiError ? uploadError : null) ??
          t("logoUploadFailed"),
      );
    } finally {
      setUploading(false);
    }
  }

  async function handleRemove() {
    setRemoving(true);
    try {
      const updated = await fetchJSON<{ logo_url: string | null }>("/api/settings/organization", {
        method: "PATCH",
        body: JSON.stringify({ name: organizationName, logo_image_key: "" }),
      });
      onUpdated(updated.logo_url);
      toast.success(t("logoRemovedToast"));
    } catch (removeError) {
      toast.error(
        apiErrorMessage(errorCopy, removeError instanceof ApiError ? removeError : null) ??
          t("logoRemoveFailed"),
      );
    } finally {
      setRemoving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("logoTitle")}</CardTitle>
        <CardDescription>{t("logoDescription", LOGO_LIMITS)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {logoUrl ? (
          <div className="flex flex-wrap gap-3">
            <div className="space-y-1">
              <p className="text-xs font-medium text-muted-foreground">{t("logoOnLight")}</p>
              <div className="flex h-24 w-40 items-center justify-center rounded-md border bg-background p-3">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={logoUrl}
                  alt={t("logoOnLightAlt")}
                  className="max-h-full max-w-full object-contain"
                />
              </div>
            </div>
            <div className="space-y-1">
              <p className="text-xs font-medium text-muted-foreground">{t("logoOnDark")}</p>
              <div className="flex h-24 w-40 items-center justify-center rounded-md border bg-zinc-900 p-3">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={logoUrl}
                  alt={t("logoOnDarkAlt")}
                  className="max-h-full max-w-full object-contain"
                />
              </div>
            </div>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t("logoNone")}</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={uploading || removing}
            onClick={() => inputRef.current?.click()}
          >
            {uploading ? t("logoUploading") : logoUrl ? t("logoReplace") : t("logoUpload")}
          </Button>
          {logoUrl ? (
            <Button
              type="button"
              variant="ghost"
              disabled={uploading || removing}
              onClick={() => void handleRemove()}
            >
              {removing ? t("logoRemoving") : t("logoRemove")}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
