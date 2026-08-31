// PROTOTYPE — throwaway route for issue #543 (Legal Center map, issue #540).
// Not linked from the operator nav on purpose. Visit /operator/legal-prototype.
import { Suspense } from "react";
import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";
import { LegalPrototypeClient } from "./legal-prototype-client";

export default async function LegalPrototypePage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }
  return (
    <StaffPageShell activePath="/operator">
      <div className="mx-auto max-w-6xl">
        <Suspense>
          <LegalPrototypeClient />
        </Suspense>
      </div>
    </StaffPageShell>
  );
}
