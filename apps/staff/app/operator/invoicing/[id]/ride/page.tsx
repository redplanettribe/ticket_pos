import { notFound } from "next/navigation";

import { loadSession } from "../../../../staff-page-shell";

import { OperatorInvoiceRideClient } from "./operator-invoice-ride-client";

type PageProps = {
  params: Promise<{ id: string }>;
};

// The RIDE (#456): a print-styled rendering of one Tax Invoice, deliberately
// outside the staff shell so what prints is the document and nothing else.
export default async function OperatorInvoiceRidePage({ params }: PageProps) {
  const session = await loadSession();
  if (!session?.is_platform_operator) {
    notFound();
  }
  const { id } = await params;
  return <OperatorInvoiceRideClient invoiceId={id} />;
}
