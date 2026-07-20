"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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
  ApiError,
  getEventTags,
  isValidTagName,
  listPopularTags,
  searchTags,
  setEventTags,
  tagCanonicalKey,
  TAG_NAME_MAX_LENGTH,
  type Tag,
} from "@/lib/events-api";

type EventTagsSectionProps = {
  eventId: string;
};

function sortTags(tags: Tag[]): Tag[] {
  return [...tags].sort((a, b) => {
    if (a.curated !== b.curated) {
      return a.curated ? -1 : 1;
    }
    return a.name.localeCompare(b.name);
  });
}

function sameTagSet(a: Tag[], b: Tag[]): boolean {
  if (a.length !== b.length) {
    return false;
  }
  const keys = new Set(a.map((tag) => tagCanonicalKey(tag.name)));
  return b.every((tag) => keys.has(tagCanonicalKey(tag.name)));
}

export function EventTagsSection({ eventId }: EventTagsSectionProps) {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState<Tag[]>([]);
  const [tags, setTags] = useState<Tag[]>([]);
  const [query, setQuery] = useState("");
  const [suggestions, setSuggestions] = useState<Tag[]>([]);
  const [presets, setPresets] = useState<Tag[]>([]);
  const [popular, setPopular] = useState<Tag[]>([]);
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const loadTags = useCallback(async () => {
    setLoading(true);
    try {
      const current = sortTags(await getEventTags(eventId));
      setSaved(current);
      setTags(current);
    } catch (loadError) {
      toast.error(loadError instanceof Error ? loadError.message : "Failed to load tags");
    } finally {
      setLoading(false);
    }
  }, [eventId]);

  useEffect(() => {
    void loadTags();
  }, [loadTags]);

  // Load the Preset Tags once so they are visible at all times — steering the
  // organizer toward reusing an existing Tag rather than coining a near-duplicate.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const results = await searchTags("");
        if (!cancelled) {
          setPresets(sortTags(results.filter((tag) => tag.curated)));
        }
      } catch {
        // Presets are a convenience; the search box still works without them.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Load the most-used Custom Tags across all Organizations so the organizer can
  // browse existing global vocabulary on focus, before typing.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const results = await listPopularTags();
        if (!cancelled) {
          setPopular(results);
        }
      } catch {
        // Browse suggestions are a convenience; the search box still works.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Typeahead search against the shared Tag pool, debounced.
  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) {
      setSuggestions([]);
      return;
    }
    let cancelled = false;
    const handle = setTimeout(async () => {
      try {
        const results = await searchTags(trimmed);
        if (!cancelled) {
          setSuggestions(results);
        }
      } catch {
        if (!cancelled) {
          setSuggestions([]);
        }
      }
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [query]);

  // Close the dropdown when clicking outside.
  useEffect(() => {
    function onClick(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, []);

  const selectedKeys = useMemo(() => new Set(tags.map((tag) => tagCanonicalKey(tag.name))), [tags]);

  const availableSuggestions = useMemo(
    () => suggestions.filter((tag) => !selectedKeys.has(tagCanonicalKey(tag.name))),
    [suggestions, selectedKeys],
  );

  // Popular Custom Tags from across all Organizations, shown on focus before typing.
  const availablePopular = useMemo(
    () => popular.filter((tag) => !selectedKeys.has(tagCanonicalKey(tag.name))),
    [popular, selectedKeys],
  );

  const trimmedQuery = query.trim();
  const browsing = trimmedQuery.length === 0;
  const dropdownItems = browsing ? availablePopular : availableSuggestions;
  const queryKey = tagCanonicalKey(query);
  const queryMatchesExisting =
    availableSuggestions.some((tag) => tagCanonicalKey(tag.name) === queryKey) ||
    selectedKeys.has(queryKey);
  const canCreate = trimmedQuery.length > 0 && !queryMatchesExisting && isValidTagName(trimmedQuery);
  const dirty = !sameTagSet(saved, tags);

  function addTag(tag: Tag) {
    const key = tagCanonicalKey(tag.name);
    if (selectedKeys.has(key)) {
      return;
    }
    setTags((current) => sortTags([...current, tag]));
    setQuery("");
    setSuggestions([]);
    setOpen(false);
  }

  function createTag() {
    if (!canCreate) {
      return;
    }
    // A Custom Tag is coined server-side on save; it is not curated.
    addTag({ name: trimmedQuery, curated: false });
  }

  function removeTag(name: string) {
    const key = tagCanonicalKey(name);
    setTags((current) => current.filter((tag) => tagCanonicalKey(tag.name) !== key));
  }

  function togglePreset(tag: Tag) {
    if (selectedKeys.has(tagCanonicalKey(tag.name))) {
      removeTag(tag.name);
    } else {
      addTag(tag);
    }
  }

  async function handleSave() {
    setSaving(true);
    try {
      const result = sortTags(await setEventTags(eventId, tags.map((tag) => tag.name)));
      setSaved(result);
      setTags(result);
      toast.success("Tags updated");
    } catch (saveError) {
      const message = saveError instanceof ApiError ? saveError.message : "Failed to update tags";
      toast.error(message);
    } finally {
      setSaving(false);
    }
  }

  const showDropdown = open && (dropdownItems.length > 0 || canCreate);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Tags</CardTitle>
        <CardDescription>
          Add Preset Tags or coin Custom Tags to help customers discover this Event.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading tags...</p>
        ) : (
          <>
            {tags.length === 0 ? (
              <p className="text-sm text-muted-foreground">No tags yet.</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {tags.map((tag) => (
                  <Badge
                    key={tagCanonicalKey(tag.name)}
                    variant={tag.curated ? "secondary" : "outline"}
                    className="gap-1.5 py-1 pl-2.5 pr-1"
                  >
                    {tag.name}
                    <button
                      type="button"
                      aria-label={`Remove ${tag.name}`}
                      className="ml-0.5 flex h-4 w-4 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      onClick={() => removeTag(tag.name)}
                    >
                      <span aria-hidden>×</span>
                    </button>
                  </Badge>
                ))}
              </div>
            )}

            {presets.length > 0 ? (
              <div className="space-y-2">
                <p className="text-sm font-medium">Preset Tags</p>
                <div className="flex flex-wrap gap-2">
                  {presets.map((tag) => {
                    const active = selectedKeys.has(tagCanonicalKey(tag.name));
                    return (
                      <button
                        key={tagCanonicalKey(tag.name)}
                        type="button"
                        aria-pressed={active}
                        onClick={() => togglePreset(tag)}
                        className={
                          active
                            ? "rounded-full border border-primary bg-primary px-3 py-1 text-sm text-primary-foreground transition-colors"
                            : "rounded-full border border-input bg-background px-3 py-1 text-sm text-foreground transition-colors hover:bg-muted"
                        }
                      >
                        {tag.name}
                      </button>
                    );
                  })}
                </div>
              </div>
            ) : null}

            <div className="relative" ref={containerRef}>
              <FormField id="event-tags-search" label="Add a tag">
                <Input
                  id="event-tags-search"
                  value={query}
                  autoComplete="off"
                  maxLength={TAG_NAME_MAX_LENGTH}
                  placeholder="Search tags or type to create a Custom Tag"
                  onChange={(event) => {
                    setQuery(event.target.value);
                    setOpen(true);
                  }}
                  onFocus={() => setOpen(true)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      if (!browsing && availableSuggestions.length > 0) {
                        addTag(availableSuggestions[0]);
                      } else if (canCreate) {
                        createTag();
                      }
                    }
                  }}
                />
              </FormField>

              {showDropdown ? (
                <ul className="absolute z-10 mt-1 max-h-60 w-full overflow-auto rounded-md border bg-background py-1 shadow-md">
                  {browsing && dropdownItems.length > 0 ? (
                    <li className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
                      Used by other events
                    </li>
                  ) : null}
                  {dropdownItems.map((tag) => (
                    <li key={tagCanonicalKey(tag.name)}>
                      <button
                        type="button"
                        className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm hover:bg-muted"
                        onClick={() => addTag(tag)}
                      >
                        <span>{tag.name}</span>
                        {tag.curated ? (
                          <Badge variant="secondary" className="text-xs">
                            Preset
                          </Badge>
                        ) : null}
                      </button>
                    </li>
                  ))}
                  {canCreate ? (
                    <li>
                      <button
                        type="button"
                        className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted"
                        onClick={createTag}
                      >
                        Create <span className="font-medium">{trimmedQuery}</span>
                        <Badge variant="outline" className="ml-auto text-xs">
                          Custom Tag
                        </Badge>
                      </button>
                    </li>
                  ) : null}
                </ul>
              ) : null}
            </div>

            <Button type="button" disabled={!dirty || saving} onClick={() => void handleSave()}>
              {saving ? "Saving..." : "Save tags"}
            </Button>
          </>
        )}
      </CardContent>
    </Card>
  );
}
