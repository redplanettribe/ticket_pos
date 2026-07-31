import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorOrganizationsClient } from "./operator-organizations-client";

export default async function OperatorOrganizationsPage() {
  const session = await loadSession();
  // Same rule as the rest of the Operator Dashboard: no operator, no page. The
  // API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/organizations">
      <div className="mx-auto max-w-5xl">
        <OperatorOrganizationsClient />
      </div>
    </StaffPageShell>
  );
}
