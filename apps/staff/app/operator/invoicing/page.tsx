import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../staff-page-shell";

import { OperatorInvoicesClient } from "./operator-invoices-client";

type PageProps = {
  searchParams: Promise<{ recipient_warning?: string }>;
};

// The Recipient Warning filter is readable off the URL (#482) so the Operator
// Dashboard's count can open the list already narrowed to the warned
// documents; the client owns the state from there.
export default async function OperatorInvoicingPage({ searchParams }: PageProps) {
  const session = await loadSession();
  // Invisible to everyone else, the posture the rest of the operator surface
  // takes. The API's allowlist check (ADR 0015) is the real gate.
  if (!session?.is_platform_operator) {
    notFound();
  }
  const { recipient_warning: recipientWarning } = await searchParams;

  return (
    <StaffPageShell activePath="/operator/invoicing">
      <div className="mx-auto max-w-5xl">
        <OperatorInvoicesClient initialRecipientWarningOnly={recipientWarning === "true"} />
      </div>
    </StaffPageShell>
  );
}
