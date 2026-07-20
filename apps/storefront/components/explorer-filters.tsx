"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { Button, Input, cn } from "@ticket-pos/ui";

import type { PublicTag } from "@/lib/api";
import { WHEN_PRESETS, type WhenPreset } from "@/lib/when";

type ExplorerFiltersProps = {
  presetTags?: PublicTag[];
};

// parseTags reads the comma-separated tag selection from the URL as a set of
// lowercased tokens, so active state matches regardless of chip casing.
function parseTags(raw: string | null): Set<string> {
  return new Set(
    (raw ?? "")
      .split(",")
      .map((t) => t.trim().toLowerCase())
      .filter(Boolean),
  );
}

export function ExplorerFilters({ presetTags = [] }: ExplorerFiltersProps) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const currentWhen = (searchParams.get("when") as WhenPreset) ?? "all";
  const selectedTags = parseTags(searchParams.get("tags"));
  const [query, setQuery] = useState(searchParams.get("q") ?? "");

  function pushParams(next: { q?: string; when?: WhenPreset; tags?: string[] }) {
    const params = new URLSearchParams(searchParams.toString());
    params.delete("cursor");

    if (next.q !== undefined) {
      if (next.q.trim()) params.set("q", next.q.trim());
      else params.delete("q");
    }
    if (next.when !== undefined) {
      if (next.when === "all") params.delete("when");
      else params.set("when", next.when);
    }
    if (next.tags !== undefined) {
      if (next.tags.length > 0) params.set("tags", next.tags.join(","));
      else params.delete("tags");
    }

    const suffix = params.toString();
    router.push(suffix ? `${pathname}?${suffix}` : pathname);
  }

  function toggleTag(tag: PublicTag) {
    const key = tag.name.toLowerCase();
    const next = new Set(selectedTags);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    pushParams({ tags: Array.from(next) });
  }

  return (
    <div className="space-y-4">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          pushParams({ q: query });
        }}
        className="flex gap-2"
        role="search"
      >
        <Input
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search events or organizers"
          aria-label="Search events or organizers"
        />
        <Button type="submit">Search</Button>
      </form>

      <div className="flex flex-wrap gap-2">
        {WHEN_PRESETS.map((preset) => {
          const active = preset.value === currentWhen;
          return (
            <button
              key={preset.value}
              type="button"
              onClick={() => pushParams({ when: preset.value })}
              aria-pressed={active}
              className={cn(
                "rounded-full border px-3 py-1 text-sm transition-colors",
                active
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-input bg-background text-foreground hover:bg-muted",
              )}
            >
              {preset.label}
            </button>
          );
        })}
      </div>

      {presetTags.length > 0 ? (
        <div className="flex flex-wrap gap-2" aria-label="Filter by tag">
          {presetTags.map((tag) => {
            const active = selectedTags.has(tag.name.toLowerCase());
            return (
              <button
                key={tag.name}
                type="button"
                onClick={() => toggleTag(tag)}
                aria-pressed={active}
                className={cn(
                  "rounded-full border px-3 py-1 text-sm transition-colors",
                  active
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-input bg-background text-foreground hover:bg-muted",
                )}
              >
                {tag.name}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
