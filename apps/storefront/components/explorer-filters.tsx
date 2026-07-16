"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { Button, Input, cn } from "@ticket-pos/ui";

import { WHEN_PRESETS, type WhenPreset } from "@/lib/when";

export function ExplorerFilters() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const currentWhen = (searchParams.get("when") as WhenPreset) ?? "all";
  const [query, setQuery] = useState(searchParams.get("q") ?? "");

  function pushParams(next: { q?: string; when?: WhenPreset }) {
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

    const suffix = params.toString();
    router.push(suffix ? `${pathname}?${suffix}` : pathname);
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
    </div>
  );
}
