"use client";

import { useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { Button, Input, cn } from "@ticket-pos/ui";

import { usePathname, useRouter } from "@/i18n/navigation";
import type { PublicTag } from "@/lib/api";
import { tagName, type TagTranslator } from "@/lib/tag-name";
import { WHEN_PRESETS, isWhenPreset, type WhenPreset } from "@/lib/when";

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
  const t = useTranslations("explorer");
  // A Preset Tag is the system's word, so it is worded here rather than by the
  // API; a Custom Tag passes through untouched (ADR 0027).
  const tTags = useTranslations("tags") as TagTranslator;
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  // Guarded rather than cast: the page falls back to "all" for a `when` it does
  // not recognise, and a bare cast would leave the chip bar disagreeing with the
  // results it labels — no chip lit while the unfiltered list is on screen.
  const whenParam = searchParams.get("when") ?? undefined;
  const currentWhen: WhenPreset = isWhenPreset(whenParam) ? whenParam : "all";
  const selectedTags = parseTags(searchParams.get("tags"));

  // The box holds a draft the Customer is still typing, so it cannot simply
  // mirror the URL — but it must follow the URL when the URL moves on its own,
  // which is what Back and Forward do. Tracking the applied `q` distinguishes
  // the two: keystrokes leave it alone, a navigation resets the draft to what
  // the results on screen were actually searched for.
  const urlQuery = searchParams.get("q") ?? "";
  const [query, setQuery] = useState(urlQuery);
  const [appliedQuery, setAppliedQuery] = useState(urlQuery);
  if (appliedQuery !== urlQuery) {
    setAppliedQuery(urlQuery);
    setQuery(urlQuery);
  }

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
    // The Tag's own canonical key, not its rendered label lowercased: the label
    // is in the page's Locale, and "Artes y teatro" lowercases to a token the
    // API matches nothing against. The key is what `tags=` has always carried
    // and what the API filters on, so it stays English in both Locales and a
    // filtered link survives being read in the other one.
    const key = tag.canonical_key;
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
          placeholder={t("searchLabel")}
          aria-label={t("searchLabel")}
        />
        <Button type="submit">{t("searchSubmit")}</Button>
      </form>

      <div className="flex flex-wrap gap-2">
        {WHEN_PRESETS.map((preset) => {
          const active = preset === currentWhen;
          return (
            <button
              key={preset}
              type="button"
              onClick={() => pushParams({ when: preset })}
              aria-pressed={active}
              className={cn(
                "rounded-full border px-3 py-1 text-sm transition-colors",
                active
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-input bg-background text-foreground hover:bg-muted",
              )}
            >
              {/* The preset's own token is the message key, so a preset added
                  to lib/when.ts fails to compile until its label is written. */}
              {t(`when.${preset}`)}
            </button>
          );
        })}
      </div>

      {presetTags.length > 0 ? (
        <div className="flex flex-wrap gap-2" aria-label={t("tagFilterLabel")}>
          {presetTags.map((tag) => {
            const active = selectedTags.has(tag.canonical_key);
            return (
              <button
                key={tag.canonical_key}
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
                {tagName(tag, tTags)}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
