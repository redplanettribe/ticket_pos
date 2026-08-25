import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorNewInvoiceClient } from "./operator-new-invoice-client";

export default async function OperatorNewInvoicePage() {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }
  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-3xl">
        <OperatorNewInvoiceClient />
      </div>
    </StaffPageShell>
  );
}
