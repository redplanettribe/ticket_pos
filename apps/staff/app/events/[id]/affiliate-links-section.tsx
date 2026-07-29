"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";

import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  toast,
} from "@ticket-pos/ui";

import {
  AFFILIATE_LINK_NAME_MAX_LENGTH,
  createAffiliateLink,
  listAffiliateLinks,
  type AffiliateLink,
} from "@/lib/affiliates-api";

type AffiliateLinksSectionProps = {
  eventId: string;
};

export function AffiliateLinksSection({ eventId }: AffiliateLinksSectionProps) {
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [links, setLinks] = useState<AffiliateLink[]>([]);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const loadLinks = useCallback(async () => {
    setLoading(true);
    try {
      setLinks(await listAffiliateLinks(eventId));
    } catch (loadError) {
      toast.error(loadError instanceof Error ? loadError.message : "Failed to load affiliate links");
    } finally {
      setLoading(false);
    }
  }, [eventId]);

  useEffect(() => {
    void loadLinks();
  }, [loadLinks]);

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      return;
    }
    setCreating(true);
    try {
      await createAffiliateLink(eventId, trimmed);
      setName("");
      await loadLinks();
      toast.success("Affiliate link created");
    } catch (createError) {
      toast.error(createError instanceof Error ? createError.message : "Failed to create affiliate link");
    } finally {
      setCreating(false);
    }
  }

  async function copyURL(link: AffiliateLink) {
    try {
      await navigator.clipboard.writeText(link.url);
      setCopiedId(link.id);
      window.setTimeout(() => setCopiedId((current) => (current === link.id ? null : current)), 2000);
      toast.success("Link copied");
    } catch {
      // Clipboard access can be refused (insecure origin, denied permission).
      // The URL is on screen and selectable, so say so rather than fail mutely.
      toast.error("Could not copy — select the link and copy it manually");
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Affiliate Links</CardTitle>
        <CardDescription>
          Named links to this Event&apos;s page that attribute Online Sales to whoever is promoting it. The
          code is generated for you and never changes.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <form className="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={(event) => void handleCreate(event)}>
          <div className="flex-1">
            <FormField id="affiliate-link-name" label="Name">
              <Input
                id="affiliate-link-name"
                value={name}
                maxLength={AFFILIATE_LINK_NAME_MAX_LENGTH}
                onChange={(event) => setName(event.target.value)}
                placeholder="María's Instagram"
                required
              />
            </FormField>
          </div>
          <Button type="submit" disabled={creating || name.trim() === ""}>
            {creating ? "Creating..." : "Create affiliate link"}
          </Button>
        </form>

        {loading ? (
          <p className="text-sm text-muted-foreground">Loading affiliate links...</p>
        ) : links.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No affiliate links yet. Create one to give a promoter their own link to this Event.
          </p>
        ) : (
          <div className="space-y-3">
            {links.map((link) => (
              <div
                key={link.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="min-w-0">
                  <p className="font-medium">{link.name}</p>
                  <p className="break-all text-sm text-muted-foreground">{link.url}</p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {/* Clicks sit next to the link so a bad link reads differently
                      from a bad audience: no clicks means nobody followed it. */}
                  <span className="text-sm text-muted-foreground">
                    <span className="font-medium text-foreground">{link.clicks}</span>{" "}
                    {link.clicks === 1 ? "click" : "clicks"}
                  </span>
                  <Badge variant={link.active ? "default" : "secondary"}>
                    {link.active ? "Active" : "Inactive"}
                  </Badge>
                  <Button type="button" variant="outline" size="sm" onClick={() => void copyURL(link)}>
                    {copiedId === link.id ? "Copied" : "Copy link"}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
