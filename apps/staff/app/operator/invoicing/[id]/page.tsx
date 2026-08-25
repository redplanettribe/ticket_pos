import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../staff-page-shell";

import { OperatorInvoiceClient } from "./operator-invoice-client";

type PageProps = {
  params: Promise<{ id: string }>;
};

export default async function OperatorInvoicePage({ params }: PageProps) {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }
  const { id } = await params;
  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-3xl">
        <OperatorInvoiceClient invoiceId={id} />
      </div>
    </StaffPageShell>
  );
}
