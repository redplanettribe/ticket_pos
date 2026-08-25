import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorIssuerClient } from "./operator-issuer-client";

export default async function OperatorInvoicingIssuerPage() {
  const session = await loadSession();
  // Same rule as the rest of the Operator Dashboard: no operator, no page. The
  // API's allowlist check (ADR 0015) is the real gate; the platform is the sole
  // Issuer (ADR 0059), so there is no Member-facing version of this screen.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/invoicing/issuer">
      <div className="mx-auto max-w-3xl">
        <OperatorIssuerClient />
      </div>
    </StaffPageShell>
  );
}
