import { redirect } from "next/navigation";

import { dossierBackHref } from "@/lib/customer-dossier";
import { loadEvent } from "@/lib/staff-event";

import { loadSession } from "../../../../../staff-page-shell";
import { CustomerDossierSection } from "./customer-dossier-section";

type CustomerDossierPageProps = {
  params: Promise<{ id: string; customerId: string }>;
  searchParams: Promise<{ from?: string | string[] }>;
};

/**
 * The Customer Dossier (#638; CONTEXT.md "Customer Dossier"): one Customer as
 * this Event knows them, under the Sales layout.
 *
 * Event Staff are sent back to the list, as the Holder List sends them — the
 * link that leads here is not drawn for them, so only a pasted URL meets this.
 * The API refuses them independently; this is not the security boundary.
 */
export default async function CustomerDossierPage({ params, searchParams }: CustomerDossierPageProps) {
  const { id, customerId } = await params;
  const resolvedSearchParams = await searchParams;
  const session = await loadSession();
  const role = session?.active_member?.role;

  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/sales`);
  }

  const event = await loadEvent(id);
  const from = Array.isArray(resolvedSearchParams.from)
    ? resolvedSearchParams.from[0]
    : resolvedSearchParams.from;

  return (
    <CustomerDossierSection
      eventId={id}
      customerId={customerId}
      backHref={dossierBackHref(from, id)}
      timezone={event?.timezone ?? null}
    />
  );
}
