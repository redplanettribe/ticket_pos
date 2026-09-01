import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../../staff-page-shell";

import { OperatorStaffAcceptancesClient } from "./operator-staff-acceptances-client";

// The staff acceptance browser (#565, spec #556, ADR 0067). Its customer
// neighbour's shape and its reasons.
export default async function OperatorStaffAcceptancesPage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/legal">
      <div className="mx-auto max-w-5xl">
        <OperatorStaffAcceptancesClient />
      </div>
    </StaffPageShell>
  );
}
