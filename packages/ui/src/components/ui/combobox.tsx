"use client";

import { Check, ChevronsUpDown, Search } from "lucide-react";
import * as React from "react";

import { cn } from "../../lib/utils";

export type ComboboxOption = {
  /** The stored value, e.g. an IANA timezone id. */
  value: string;
  /** Primary text shown to the user and matched first when searching. */
  label: string;
  /** Optional muted right-aligned text (e.g. a GMT offset). Also searchable. */
  hint?: string;
};

type ComboboxProps = {
  options: ComboboxOption[];
  value: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyText?: string;
  className?: string;
  disabled?: boolean;
  /** Injected by FormField. */
  id?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
};

function matches(option: ComboboxOption, query: string): boolean {
  if (!query) return true;
  const haystack = `${option.label} ${option.value} ${option.hint ?? ""}`.toLowerCase();
  // Every whitespace-separated token must appear, so "new eur" narrows sensibly.
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((token) => haystack.includes(token));
}

export function Combobox({
  options,
  value,
  onValueChange,
  placeholder = "Select…",
  searchPlaceholder = "Search…",
  emptyText = "No results found.",
  className,
  disabled,
  id,
  "aria-describedby": ariaDescribedBy,
  "aria-invalid": ariaInvalid,
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [activeIndex, setActiveIndex] = React.useState(0);

  const containerRef = React.useRef<HTMLDivElement>(null);
  const inputRef = React.useRef<HTMLInputElement>(null);
  const listRef = React.useRef<HTMLUListElement>(null);

  const selected = React.useMemo(() => options.find((o) => o.value === value) ?? null, [options, value]);

  const filtered = React.useMemo(() => options.filter((o) => matches(o, query)), [options, query]);

  // Close on outside pointer down.
  React.useEffect(() => {
    if (!open) return;
    function onPointerDown(event: PointerEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  // When opening, focus the search box and highlight the current selection.
  React.useEffect(() => {
    if (!open) {
      setQuery("");
      return;
    }
    inputRef.current?.focus();
    const idx = selected ? filtered.findIndex((o) => o.value === selected.value) : 0;
    setActiveIndex(idx >= 0 ? idx : 0);
    // Only when transitioning to open.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Keep the active option scrolled into view.
  React.useEffect(() => {
    if (!open) return;
    const list = listRef.current;
    const active = list?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`);
    active?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, open]);

  const commit = React.useCallback(
    (option: ComboboxOption | undefined) => {
      if (!option) return;
      onValueChange(option.value);
      setOpen(false);
    },
    [onValueChange],
  );

  function onInputKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, filtered.length - 1));
        break;
      case "ArrowUp":
        event.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
        break;
      case "Enter":
        event.preventDefault();
        commit(filtered[activeIndex]);
        break;
      case "Escape":
        event.preventDefault();
        setOpen(false);
        break;
      case "Home":
        event.preventDefault();
        setActiveIndex(0);
        break;
      case "End":
        event.preventDefault();
        setActiveIndex(filtered.length - 1);
        break;
    }
  }

  const listboxId = id ? `${id}-listbox` : undefined;

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <button
        type="button"
        id={id}
        role="combobox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-describedby={ariaDescribedBy}
        aria-invalid={ariaInvalid}
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          "flex h-10 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
          "disabled:cursor-not-allowed disabled:opacity-50",
          ariaInvalid && "border-destructive",
        )}
      >
        <span className={cn("flex min-w-0 items-center gap-2", !selected && "text-muted-foreground")}>
          {selected ? (
            <>
              <span className="truncate">{selected.label}</span>
              {selected.hint ? <span className="shrink-0 text-xs text-muted-foreground">{selected.hint}</span> : null}
            </>
          ) : (
            placeholder
          )}
        </span>
        <ChevronsUpDown className="size-4 shrink-0 opacity-50" aria-hidden />
      </button>

      {open ? (
        <div className="absolute z-50 mt-1 w-full overflow-hidden rounded-md border border-input bg-popover text-popover-foreground shadow-md">
          <div className="flex items-center gap-2 border-b border-border px-3">
            <Search className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <input
              ref={inputRef}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setActiveIndex(0);
              }}
              onKeyDown={onInputKeyDown}
              placeholder={searchPlaceholder}
              className="h-10 w-full bg-transparent py-2 text-sm outline-none placeholder:text-muted-foreground"
              aria-autocomplete="list"
              aria-controls={listboxId}
            />
          </div>
          {filtered.length === 0 ? (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">{emptyText}</p>
          ) : (
            <ul ref={listRef} id={listboxId} role="listbox" className="max-h-64 overflow-y-auto p-1">
              {filtered.map((option, index) => {
                const isSelected = option.value === value;
                const isActive = index === activeIndex;
                return (
                  <li
                    key={option.value}
                    data-index={index}
                    role="option"
                    aria-selected={isSelected}
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => commit(option)}
                    className={cn(
                      "flex cursor-pointer items-center gap-2 rounded-sm px-2 py-2 text-sm",
                      isActive && "bg-accent text-accent-foreground",
                    )}
                  >
                    <Check className={cn("size-4 shrink-0", isSelected ? "opacity-100" : "opacity-0")} aria-hidden />
                    <span className="min-w-0 flex-1 truncate">{option.label}</span>
                    {option.hint ? (
                      <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{option.hint}</span>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      ) : null}
    </div>
  );
}
