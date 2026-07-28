import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorSaleClient } from "./operator-sale-client";

type OperatorSalePageProps = {
  params: Promise<{ confirmationRef: string }>;
};

export default async function OperatorSalePage({ params }: OperatorSalePageProps) {
  const { confirmationRef } = await params;
  const session = await loadSession();
  // Same rule as the rest of the Operator Dashboard: no operator, no page. The
  // API's allowlist check is the real gate (ADR 0015).
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator">
      <div className="mx-auto max-w-5xl">
        <OperatorSaleClient confirmationRef={decodeURIComponent(confirmationRef)} />
      </div>
    </StaffPageShell>
  );
}
