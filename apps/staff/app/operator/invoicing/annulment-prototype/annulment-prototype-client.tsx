/* eslint-disable i18next/no-literal-string */
// PROTOTYPE - throwaway. Host for the three variants of issue #715.
"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { Breadcrumb, Button, toast } from "@ticket-pos/ui";

import { type Fixture, SCENARIOS, windowFor } from "./fixture";
import { PrototypeNote, PrototypeSwitcher, VARIANTS, type VariantKey } from "./prototype-kit";
import { VariantA } from "./variant-a-dialog";
import { VariantB } from "./variant-b-runbook";
import { VariantC } from "./variant-c-sheet";

export function AnnulmentPrototypeClient() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const rawVariant = searchParams.get("variant")?.toUpperCase();
  const variant: VariantKey = VARIANTS.some((v) => v.key === rawVariant) ? (rawVariant as VariantKey) : "A";
  const scenario = SCENARIOS.find((s) => s.key === searchParams.get("scenario")) ?? SCENARIOS[0];

  const [invoice, setInvoice] = useState<Fixture>(scenario.invoice);
  useEffect(() => setInvoice(scenario.invoice), [scenario]);

  const navigate = (next: { variant?: VariantKey; scenario?: string }) => {
    const params = new URLSearchParams({ variant: next.variant ?? variant, scenario: next.scenario ?? scenario.key });
    router.replace(`${pathname}?${params.toString()}`, { scroll: false });
  };

  const annulWindow = windowFor(invoice.issuedOn, scenario.today);
  const now = `${scenario.today}T10:15:00-05:00`;

  const onAnnul = ({ note, lateAcknowledged }: { note: string; lateAcknowledged: boolean }) => {
    setInvoice((current) => ({
      ...current,
      status: "annulled",
      annulledBy: "operator@multiticketing.ec",
      annulledAt: now,
      annulNote: note.trim() === "" ? null : note.trim(),
      lateAcknowledged,
      sriCheck: scenario.checkAnswers,
      sriCheckedAt: scenario.checkAnswers === "unknown" ? null : now,
    }));
    toast.success("Recorded as annulled at the SRI");
  };

  // A later check reads ANULADO: clearing the warning is all it does (#713 section 5).
  const onRecheck = () => {
    setInvoice((current) => ({ ...current, sriCheck: "anulado", sriCheckedAt: now }));
    toast.success("The SRI reports ANULADO");
  };

  const props = { invoice, today: scenario.today, window: annulWindow, onAnnul, onRecheck };

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: "Operator", href: "/operator" },
          { label: "Tax Invoices", href: "/operator/invoicing" },
          { label: invoice.number },
        ]}
      />
      <PrototypeNote>
        Throwaway (issue #715). Fixture data, nothing reaches the API. &quot;Today&quot; is {scenario.today} in this
        scenario. Switch variant with ← → and scenario from the bar.{" "}
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="h-6 px-2 text-xs"
          onClick={() => setInvoice(scenario.invoice)}
        >
          Reset scenario
        </Button>
      </PrototypeNote>

      {variant === "A" && <VariantA key={`A-${scenario.key}`} {...props} />}
      {variant === "B" && <VariantB key={`B-${scenario.key}`} {...props} />}
      {variant === "C" && <VariantC key={`C-${scenario.key}`} {...props} />}

      <PrototypeSwitcher current={variant} scenario={scenario.key} onChange={navigate} />
    </div>
  );
}
