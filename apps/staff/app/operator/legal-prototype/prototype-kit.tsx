/* eslint-disable i18next/no-literal-string */
// PROTOTYPE — throwaway. Shared bits across the three variants for issue #543.
"use client";

import { useEffect } from "react";
import { Badge, cn } from "@ticket-pos/ui";

import { diffHunks, wordDiff, type CellStatus } from "./fixture";

export const VARIANTS = [
  { key: "A", name: "Parallel columns" },
  { key: "B", name: "Language tabs + publish wizard" },
  { key: "C", name: "Diff-first review" },
] as const;

export type VariantKey = (typeof VARIANTS)[number]["key"];

export function PrototypeSwitcher({
  current,
  onChange,
}: {
  current: VariantKey;
  onChange: (next: VariantKey) => void;
}) {
  const index = VARIANTS.findIndex((v) => v.key === current);
  const step = (delta: number) => {
    const next = VARIANTS[(index + delta + VARIANTS.length) % VARIANTS.length];
    onChange(next.key);
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) return;
      if (event.key === "ArrowLeft") step(-1);
      if (event.key === "ArrowRight") step(1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  if (process.env.NODE_ENV === "production") return null;

  return (
    <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-full bg-neutral-900 px-3 py-2 text-sm text-white shadow-lg ring-1 ring-white/20">
      <button type="button" onClick={() => step(-1)} className="rounded-full px-2 py-1 hover:bg-white/15" aria-label="Previous variant">
        ←
      </button>
      <span className="font-mono tabular-nums">
        {current} — {VARIANTS[index]?.name}
      </span>
      <button type="button" onClick={() => step(1)} className="rounded-full px-2 py-1 hover:bg-white/15" aria-label="Next variant">
        →
      </button>
    </div>
  );
}

const STATUS_STYLE: Record<CellStatus, { label: string; className: string }> = {
  unchanged: { label: "unchanged", className: "bg-muted text-muted-foreground" },
  modified: { label: "edited", className: "bg-amber-100 text-amber-900" },
  added: { label: "new", className: "bg-emerald-100 text-emerald-900" },
  removed: { label: "removed", className: "bg-rose-100 text-rose-900" },
  missing: { label: "not written", className: "bg-rose-600 text-white" },
};

export function StatusBadge({ status, className }: { status: CellStatus; className?: string }) {
  const style = STATUS_STYLE[status];
  return <Badge className={cn("rounded-sm px-1.5 py-0 text-[11px] font-medium", style.className, className)}>{style.label}</Badge>;
}

export function WordDiff({ before, after, collapse = true }: { before: string; after: string; collapse?: boolean }) {
  const tokens = wordDiff(before, after);
  const hunks = collapse ? diffHunks(tokens) : [tokens];
  if (hunks.length === 0) {
    return <p className="text-sm text-muted-foreground">No change.</p>;
  }
  return (
    <div className="space-y-3 text-sm leading-relaxed">
      {hunks.map((hunk, hunkIndex) => (
        <div key={hunkIndex} className="rounded-md border bg-background p-3">
          {hunkIndex > 0 && <p className="mb-2 font-mono text-xs text-muted-foreground">…</p>}
          <p className="whitespace-pre-wrap break-words">
            {hunk.map((token, tokenIndex) => (
              <span
                key={tokenIndex}
                className={cn(
                  token.kind === "insert" && "rounded-sm bg-emerald-100 text-emerald-900",
                  token.kind === "delete" && "rounded-sm bg-rose-100 text-rose-900 line-through",
                )}
              >
                {token.text}
              </span>
            ))}
          </p>
        </div>
      ))}
    </div>
  );
}

export function PrototypeNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-dashed border-amber-400 bg-amber-50 px-3 py-2 text-xs text-amber-900">
      <span className="font-semibold">Prototype note — </span>
      {children}
    </p>
  );
}
