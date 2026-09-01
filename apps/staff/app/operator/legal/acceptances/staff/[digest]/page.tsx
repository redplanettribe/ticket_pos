import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../../../staff-page-shell";

import { OperatorStaffRecordClient } from "./operator-staff-record-client";

type PageProps = {
  params: Promise<{ digest: string }>;
};

// One staff person's Terms Acceptance record (#566, spec #556, ADR 0067).
//
// A CHILD OF THE STAFF BROWSER, in the route tree as on the screen. The segment
// is a STAFF DIGEST and not an address: a staff person has no id — the person
// key of the Staff platform is an email — and an address must never reach a
// URL, browser history, or the Referer header of the next click.
//
// Invisible to everyone else rather than merely forbidden: the API's allowlist
// check (ADR 0015) is the real gate, and this only decides whether the page
// exists for this session.
export default async function OperatorStaffRecordPage({ params }: PageProps) {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  const { digest } = await params;

  return (
    <StaffPageShell activePath="/operator/legal">
      <div className="mx-auto max-w-5xl">
        <OperatorStaffRecordClient digest={digest} />
      </div>
    </StaffPageShell>
  );
}
