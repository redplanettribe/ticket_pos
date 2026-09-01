import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorAccessLogClient } from "./operator-access-log-client";

// The consent access log (#569, spec #556, ADR 0067): the platform's record of
// its own reads of people's data, read where it lives.
//
// Invisible to everyone else rather than merely forbidden, the posture the whole
// operator surface takes: the API's allowlist check (ADR 0015) is the real gate,
// and this only decides whether the page exists for this session.
//
// `activePath` names the Legal Center, so the sidebar keeps its Legal Center
// entry lit while the operator is inside it — this screen is part of that
// surface and not a sibling of it.
export default async function OperatorAccessLogPage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/legal">
      <div className="mx-auto max-w-5xl">
        <OperatorAccessLogClient />
      </div>
    </StaffPageShell>
  );
}
