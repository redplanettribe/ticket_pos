/* eslint-disable i18next/no-literal-string */
// PROTOTYPE — throwaway. Host for the three variants of issue #543.
"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button, PageHeader } from "@ticket-pos/ui";

import { initialDraft, type ArtifactSet } from "./fixture";
import { PrototypeSwitcher, VARIANTS, type VariantKey } from "./prototype-kit";
import { VariantA } from "./variant-a-parallel";
import { VariantB } from "./variant-b-tabs-wizard";
import { VariantC } from "./variant-c-diff-first";

export function LegalPrototypeClient() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const raw = searchParams.get("variant")?.toUpperCase();
  const variant: VariantKey = VARIANTS.some((v) => v.key === raw) ? (raw as VariantKey) : "A";

  const [draft, setDraft] = useState<ArtifactSet>(() => initialDraft());

  const setVariant = (next: VariantKey) => {
    router.replace(`${pathname}?variant=${next}`, { scroll: false });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="Legal Center — edition editor (PROTOTYPE)"
        description="Throwaway. Nothing here writes to the backend. Three variants; ← → or the bar at the bottom to switch."
      />
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span>Draft state is shared across variants.</span>
        <Button size="sm" variant="ghost" onClick={() => setDraft(initialDraft())}>
          Reset draft
        </Button>
      </div>

      {variant === "A" && <VariantA draft={draft} setDraft={setDraft} />}
      {variant === "B" && <VariantB draft={draft} setDraft={setDraft} />}
      {variant === "C" && <VariantC draft={draft} setDraft={setDraft} />}

      <PrototypeSwitcher current={variant} onChange={setVariant} />
    </div>
  );
}
