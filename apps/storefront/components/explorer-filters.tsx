"use client";

import { useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { Button, Input, cn } from "@ticket-pos/ui";

import { usePathname, useRouter } from "@/i18n/navigation";
import type { PublicTag } from "@/lib/api";
import { followIntent } from "@/lib/follow-intent";
import { tagFollowEndpoint } from "@/lib/follows";
import { tagName, toTagTranslator } from "@/lib/tag-name";
import { WHEN_PRESETS, isWhenPreset, type WhenPreset } from "@/lib/when";

import { FollowButton } from "./follow-button";

type ExplorerFiltersProps = {
  presetTags?: PublicTag[];
  /**
   * The canonical keys of the Tags this Customer already Follows, as the server
   * rendered them, and `null` when nobody is signed in or the Follows could not
   * be read (#218).
   *
   * Null rather than an empty array, because the two are different: an empty
   * array is "signed in, follows no Tag" and draws the controls unpressed, while
   * null is "we cannot say" and draws none at all. Passed down from the page
   * rather than fetched here — one read answers the whole chip bar, and this is
   * a client component that must never hold a session token.
   */
  followedTagKeys?: string[] | null;
};

// parseTags reads the comma-separated tag selection from the URL as a set of
// canonical keys. Still lowercased, but no longer to forgive the chip's casing
// — a chip's label is in the page's Locale now and never reaches the URL. It
// forgives a hand-typed or hand-edited address, which is the only way an
// uppercase token gets here.
function parseTags(raw: string | null): Set<string> {
  return new Set(
    (raw ?? "")
      .split(",")
      .map((t) => t.trim().toLowerCase())
      .filter(Boolean),
  );
}

export function ExplorerFilters({
  presetTags = [],
  followedTagKeys = null,
}: ExplorerFiltersProps) {
  // Every visitor gets the Follow control (#219): a null read means signed out
  // or unresolvable, and either way pressing it is a way into sign-in rather
  // than a reason to hide the control from the anonymous majority this bar is
  // mostly seen by.
  const signedIn = followedTagKeys !== null;
  const t = useTranslations("explorer");
  // Preset Tag copy is the Storefront's, not the API's (ADR 0027).
  const tTags = toTagTranslator(useTranslations("tags"));
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
    // The Tag's own canonical key, not the chip's own wording lowercased: what
    // the chip reads is in the page's Locale, and "Arte y teatro" lowercases to
    // a token the API matches nothing against. The key is what `tags=` has always carried
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
            const label = tagName(tag, tTags);
            // FILTERING AND FOLLOWING ARE TWO DIFFERENT ACTS on the same word,
            // so they are two controls rather than one chip that does both.
            // Pressing the chip narrows what is on screen right now; pressing
            // Follow subscribes to the Tag for good (ADR 0030). Merging them
            // would make an ordinary browse — tap Music, look, tap it off —
            // silently sign somebody up for mail.
            // The chip carries no border of its own: the pill around both halves
            // draws the only one there is. Bordering the halves separately put a
            // rule down the middle of a single object, and a filter chip does
            // not need to be fenced off from the heart that follows it.
            // A selected Tag lights the pill's outline rather than filling the
            // word's half — a fill stopped short of the heart and read as a
            // block dropped inside the pill, not as the pill being on. Hover is
            // the same story: the chip carries no fill of its own, and marks
            // itself only so the pill around it can take the hover fill whole.
            const chip = (
              <button
                type="button"
                data-tag-chip
                onClick={() => toggleTag(tag)}
                aria-pressed={active}
                className={cn(
                  "rounded-l-full py-1 pr-2 pl-3 text-sm transition-colors",
                  active ? "font-medium text-primary" : "text-foreground",
                )}
              >
                {label}
              </button>
            );

            return (
              <span
                key={tag.canonical_key}
                // One pill, one border, two controls inside it. The heart is
                // spaced off the word by its own width rather than divided from
                // it by a line.
                className={cn(
                  "inline-flex items-center rounded-full border bg-background transition-colors",
                  active
                    ? "border-primary ring-1 ring-primary"
                    : "border-input has-[[data-tag-chip]:hover]:bg-muted",
                )}
              >
                {chip}
                {/* Drawn for anybody. A signed-out press carries the Tag
                    through sign-in and comes back made (#219). */}
                <FollowButton
                  endpoint={tagFollowEndpoint(tag.canonical_key)}
                  following={followedTagKeys?.includes(tag.canonical_key) ?? false}
                  subjectName={label}
                  intent={followIntent("tag", tag.canonical_key)}
                  signedIn={signedIn}
                  compact
                  // Rounded to the pill it closes. Its height is left alone: it
                  // sets the pill's, and the design system's shortest control is
                  // already the floor for something this small to hit.
                  className="rounded-full"
                />
              </span>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
