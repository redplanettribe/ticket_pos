import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorPayoutRequestsClient } from "./operator-payout-requests-client";

export default async function OperatorPayoutRequestsPage() {
  const session = await loadSession();
  // Invisible to everyone else, not merely forbidden — the same posture the rest
  // of the operator surface takes. The API's allowlist check (ADR 0015) is the
  // real gate; this only decides whether the page exists for this session.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/payout-requests">
      <div className="mx-auto max-w-5xl">
        <OperatorPayoutRequestsClient />
      </div>
    </StaffPageShell>
  );
}
