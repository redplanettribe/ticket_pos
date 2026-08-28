import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorUninvoicedClient } from "./operator-uninvoiced-client";

// Ventas sin factura (#507, ADR 0064): every Uninvoiced House Sale, and the
// Sale Invoice Backfill that settles them (#508). Reached from the invoicing
// list's count (#509); the client owns the state from there.
export default async function OperatorUninvoicedPage() {
  const session = await loadSession();
  // Invisible to everyone else, the posture the rest of the operator surface
  // takes. The API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-6xl">
        <OperatorUninvoicedClient />
      </div>
    </StaffPageShell>
  );
}
