import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../../../staff-page-shell";

import { OperatorCustomerRecordClient } from "./operator-customer-record-client";

type PageProps = {
  params: Promise<{ customerId: string }>;
};

// One Customer's consent record (#566, spec #556, ADR 0067).
//
// A CHILD OF THE BROWSER IT IS REACHED FROM, in the route tree as on the
// screen: the browser lists who owes an acceptance, and a row leads here. The
// segment is the Customer's OPAQUE UUID, so opening somebody's record puts no
// address in the URL bar, in the browser's history, or in the Referer header of
// whatever the operator clicks next.
//
// Invisible to everyone else rather than merely forbidden, the posture the
// whole operator surface takes: the API's allowlist check (ADR 0015) is the
// real gate, and this only decides whether the page exists for this session.
//
// `activePath` names the Legal Center, so the sidebar keeps that entry lit
// while the operator is inside it.
export default async function OperatorCustomerRecordPage({ params }: PageProps) {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  const { customerId } = await params;

  return (
    <StaffPageShell activePath="/operator/legal">
      <div className="mx-auto max-w-5xl">
        <OperatorCustomerRecordClient customerId={customerId} />
      </div>
    </StaffPageShell>
  );
}
