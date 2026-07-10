import { StaffPageShell } from "../../staff-page-shell";
import { EventDetailForm } from "./event-detail-form";

type EventDetailPageProps = {
  params: Promise<{ id: string }>;
};

export default async function EventDetailPage({ params }: EventDetailPageProps) {
  const { id } = await params;

  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <EventDetailForm eventId={id} />
      </div>
    </StaffPageShell>
  );
}
