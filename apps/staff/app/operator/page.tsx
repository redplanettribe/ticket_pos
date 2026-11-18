import { notFound } from "next/navigation";

import { type PlatformTrendsSearchParams, parsePlatformTrendsParams } from "@/lib/platform-trends";

import { StaffPageShell, loadSession } from "../staff-page-shell";

import { OperatorDashboardClient } from "./operator-dashboard-client";

type PageProps = {
  searchParams: Promise<PlatformTrendsSearchParams>;
};

// The Platform Trends reading (its span and its comparison) is read off the
// address here and handed to the client (#821), as the Tax Invoices list reads
// its filters: the address is the source of truth, so a reload or a link sent
// to another operator shows the same reading.
export default async function OperatorPage({ searchParams }: PageProps) {
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
        <OperatorDashboardClient trendsView={parsePlatformTrendsParams(await searchParams)} />
      </div>
    </StaffPageShell>
  );
}
