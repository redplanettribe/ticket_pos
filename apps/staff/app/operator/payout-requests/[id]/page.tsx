import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorPayoutRequestClient } from "./operator-payout-request-client";

type PageProps = {
  params: Promise<{ id: string }>;
};

export default async function OperatorPayoutRequestPage({ params }: PageProps) {
  const session = await loadSession();
  // Invisible to everyone else, not merely forbidden. The API's allowlist check
  // (ADR 0015) is the real gate — and it matters here more than anywhere: this
  // is the one page on the platform that shows a whole bank account number.
  if (!session?.is_platform_operator) {
    notFound();
  }

  const { id } = await params;

  return (
    <StaffPageShell activePath="/operator/payout-requests">
      <div className="mx-auto max-w-3xl">
        <OperatorPayoutRequestClient requestId={id} />
      </div>
    </StaffPageShell>
  );
}
