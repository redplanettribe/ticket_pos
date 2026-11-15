// PROTOTYPE - throwaway route for issue #715 (SRI annulment map, issue #710).
// Not linked from the operator nav on purpose. Visit
// /operator/invoicing/annulment-prototype?variant=A&scenario=open
import { Suspense } from "react";
import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { AnnulmentPrototypeClient } from "./annulment-prototype-client";

export default async function AnnulmentPrototypePage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }
  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-3xl pb-24">
        <Suspense>
          <AnnulmentPrototypeClient />
        </Suspense>
      </div>
    </StaffPageShell>
  );
}
