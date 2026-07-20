import { StaffPageShell, loadSession } from "../../staff-page-shell";
import { EventDetailForm } from "./event-detail-form";

type EventDetailPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventDetailPage({ params }: EventDetailPageProps) {
  const { id } = await params;
  const session = await loadSession();
  const isOrgAdmin = session?.active_member?.role === "org_admin";

  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <EventDetailForm eventId={id} isOrgAdmin={isOrgAdmin} />
      </div>
    </StaffPageShell>
  );
}
