import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorConsentClient } from "./operator-consent-client";

export default async function OperatorConsentPage() {
  const session = await loadSession();
  // Same rule as the rest of the Operator Dashboard: no operator, no page. The
  // API's allowlist check (ADR 0015) is the real gate, and here it is doing more
  // than hiding a screen — Customer identity is global and separate from staff
  // (ADR 0010), so this is the boundary that stops an Organization reaching the
  // consents of people who bought from other venues.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/consent">
      <div className="mx-auto max-w-3xl">
        <OperatorConsentClient />
      </div>
    </StaffPageShell>
  );
}
