import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorLegalClient } from "./operator-legal-client";

/**
 * The Legal Center (#561, spec #556): the platform's own agreements, and the one
 * mutable draft of each.
 *
 * NO MIDDLEWARE CHANGE PROTECTS THIS ROUTE, and none is needed:
 * `apps/staff/middleware.ts` already gates the whole `/operator` subtree through
 * `operatorPaths`, before the membership fork (ADR 0015 — operator authority is
 * orthogonal to Membership), and redirects a non-operator to `/` rather than
 * showing a 403, so the surface is invisible rather than merely forbidden. The
 * check below decides whether the page exists for this session; the API's
 * allowlist is the real gate.
 */
export default async function OperatorLegalPage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/legal">
      {/* Wider than the other operator surfaces: this one is two columns of
          prose side by side, and #543 chose parallel columns wholesale. */}
      <div className="mx-auto max-w-7xl">
        <OperatorLegalClient />
      </div>
    </StaffPageShell>
  );
}
