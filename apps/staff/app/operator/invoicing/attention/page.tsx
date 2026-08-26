import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorAttentionClient } from "./operator-attention-client";

export default async function OperatorAttentionPage() {
  const session = await loadSession();
  // Invisible to everyone else, the posture the rest of the operator surface
  // takes. The API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-5xl">
        <OperatorAttentionClient />
      </div>
    </StaffPageShell>
  );
}
