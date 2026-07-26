import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../staff-page-shell";

import { OperatorDashboardClient } from "./operator-dashboard-client";

export default async function OperatorPage() {
  const session = await loadSession();
  // The operator surface is invisible to everyone else, not merely forbidden.
  // The API's allowlist check (ADR 0015) is the real gate; this only decides
  // whether the page exists for this session.
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator">
      <div className="mx-auto max-w-5xl">
        <OperatorDashboardClient />
      </div>
    </StaffPageShell>
  );
}
