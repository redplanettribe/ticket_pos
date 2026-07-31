import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorSaleLookupClient } from "./operator-sale-lookup-client";

export default async function OperatorSalesPage() {
  const session = await loadSession();
  // Same rule as the rest of the Operator Dashboard: no operator, no page. The
  // API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/sales">
      <div className="mx-auto max-w-5xl">
        <OperatorSaleLookupClient />
      </div>
    </StaffPageShell>
  );
}
