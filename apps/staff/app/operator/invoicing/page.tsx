import { notFound } from "next/navigation";

import {
  type OperatorInvoiceListSearchParams,
  parseOperatorInvoiceListParams,
} from "@/lib/operator-invoice-list";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorInvoicesClient } from "./operator-invoices-client";

type PageProps = {
  searchParams: Promise<OperatorInvoiceListSearchParams>;
};

// The list's whole narrowing is read off the URL here and handed to the client
// as its state (#594): the Kind, the Status, the Recipient Warning, the page —
// and, beside them rather than among them, the order the page is read in
// (#597), which narrows nothing.
// The address bar is the source of truth, so a narrowed view is a link a
// colleague can open, survives a reload, and walks the back button — and the
// Operator Dashboard's `?recipient_warning=true` link (#482) is no longer a
// one-time hint into component state but the ordinary path everything takes.
export default async function OperatorInvoicingPage({ searchParams }: PageProps) {
  const session = await loadSession();
  // Invisible to everyone else, the posture the rest of the operator surface
  // takes. The API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }
  const { page, filters, sort, dir } = parseOperatorInvoiceListParams(await searchParams);

  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-5xl">
        <OperatorInvoicesClient page={page} filters={filters} sort={sort} dir={dir} />
      </div>
    </StaffPageShell>
  );
}
