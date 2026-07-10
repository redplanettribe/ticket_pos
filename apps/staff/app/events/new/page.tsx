import { StaffPageShell } from "../../staff-page-shell";
import { CreateEventForm } from "./create-event-form";

export default async function NewEventPage() {
  return (
    <StaffPageShell activePath="/events">
      <div className="mx-auto max-w-4xl">
        <CreateEventForm />
      </div>
    </StaffPageShell>
  );
}
