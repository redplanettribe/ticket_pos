import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../../staff-page-shell";

import { OperatorCustomerAcceptancesClient } from "./operator-customer-acceptances-client";

// The customer acceptance browser (#565, spec #556, ADR 0067).
//
// Invisible to everyone else rather than merely forbidden, which is the posture
// the whole operator surface takes: the API's allowlist check (ADR 0015) is the
// real gate, and this only decides whether the page exists for this session.
//
// `activePath` names the Legal Center rather than this page, so the sidebar
// keeps its Legal Center entry lit while the operator is inside it — these two
// browsers are part of that surface and not siblings of it.
export default async function OperatorCustomerAcceptancesPage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/legal">
      <div className="mx-auto max-w-5xl">
        <OperatorCustomerAcceptancesClient />
      </div>
    </StaffPageShell>
  );
}
