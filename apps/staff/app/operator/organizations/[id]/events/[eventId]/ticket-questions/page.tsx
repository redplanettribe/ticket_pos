import { notFound } from "next/navigation";

import { StaffPageShell, loadSession } from "../../../../../../staff-page-shell";

import { OperatorEventTicketQuestionsClient } from "./operator-event-ticket-questions-client";

type OperatorEventTicketQuestionsPageProps = {
  params: Promise<{ id: string; eventId: string }>;
};

// The Operator's view of an Event's Ticket Questions, and the Revoke control
// (#410, ADR 0056).
export default async function OperatorEventTicketQuestionsPage({
  params,
}: OperatorEventTicketQuestionsPageProps) {
  const { id, eventId } = await params;
  const session = await loadSession();
  // Same rule as the dashboard index: no operator, no page (ADR 0015).
  if (!session?.is_platform_operator) {
    notFound();
  }

  return (
    <StaffPageShell activePath="/operator/organizations">
      <div className="mx-auto max-w-5xl">
        <OperatorEventTicketQuestionsClient organizationId={id} eventId={eventId} />
      </div>
    </StaffPageShell>
  );
}
