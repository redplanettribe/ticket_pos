import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorOrganizationClient } from "./operator-organization-client";

type OperatorOrganizationPageProps = {
  params: Promise<{ id: string }>;
};

export default async function OperatorOrganizationPage({ params }: OperatorOrganizationPageProps) {
  const { id } = await params;
  const session = await loadSession();
  // Same rule as the dashboard index: no operator, no page (ADR 0015).
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/organizations">
      <div className="mx-auto max-w-5xl">
        <OperatorOrganizationClient organizationId={id} />
      </div>
    </StaffPageShell>
  );
}
