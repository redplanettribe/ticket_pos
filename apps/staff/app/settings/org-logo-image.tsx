"use client";

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

const MAX_LOGO_BYTES = 5 * 1024 * 1024;
const ACCEPTED_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);
const MIN_LOGO_DIMENSION = 64;

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
      reject(new Error("Could not read image dimensions"));
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
  error: { code: string; message: string } | null;
};

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new Error(envelope.error?.message ?? "Request failed");
  }
  if (envelope.data === null) {
    throw new Error("Empty response");
  }
  return envelope.data;
}

type OrgLogoImageProps = {
  organizationName: string;
  logoUrl: string | null;
  onUpdated: (logoUrl: string | null) => void;
};

export function OrgLogoImage({ organizationName, logoUrl, onUpdated }: OrgLogoImageProps) {
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
      toast.error("Logo must be JPEG, PNG, or WebP.");
      return;
    }
    if (file.size > MAX_LOGO_BYTES) {
      toast.error("Logo must be 5 MB or smaller.");
      return;
    }

    try {
      const { width, height } = await readImageDimensions(file);
      if (Math.min(width, height) < MIN_LOGO_DIMENSION) {
        toast.error("Logo must be at least 64px on its shorter side.");
        return;
      }
    } catch {
      toast.error("Could not read the selected image.");
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
        throw new Error("Failed to upload logo.");
      }

      await attachLogo(presign.object_key);
      toast.success("Logo uploaded");
    } catch (uploadError) {
      toast.error(uploadError instanceof Error ? uploadError.message : "Failed to upload logo");
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
      toast.success("Logo removed");
    } catch (removeError) {
      toast.error(removeError instanceof Error ? removeError.message : "Failed to remove logo");
    } finally {
      setRemoving(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Logo</CardTitle>
        <CardDescription>
          JPEG, PNG, or WebP up to 5 MB, at least 64px on the shorter side. Shown in Staff and on
          the Storefront.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {logoUrl ? (
          <div className="flex flex-wrap gap-3">
            <div className="space-y-1">
              <p className="text-xs font-medium text-muted-foreground">Light</p>
              <div className="flex h-24 w-40 items-center justify-center rounded-md border bg-background p-3">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={logoUrl} alt="Organization logo preview on a light background" className="max-h-full max-w-full object-contain" />
              </div>
            </div>
            <div className="space-y-1">
              <p className="text-xs font-medium text-muted-foreground">Dark</p>
              <div className="flex h-24 w-40 items-center justify-center rounded-md border bg-zinc-900 p-3">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={logoUrl} alt="Organization logo preview on a dark background" className="max-h-full max-w-full object-contain" />
              </div>
            </div>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No logo yet.</p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className="hidden"
          onChange={(event) => void handleFileChange(event)}
        />

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={uploading || removing} onClick={() => inputRef.current?.click()}>
            {uploading ? "Uploading..." : logoUrl ? "Replace logo" : "Upload logo"}
          </Button>
          {logoUrl ? (
            <Button type="button" variant="ghost" disabled={uploading || removing} onClick={() => void handleRemove()}>
              {removing ? "Removing..." : "Remove logo"}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
