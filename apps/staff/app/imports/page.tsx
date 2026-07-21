import { PageHeader } from "@ticket-pos/ui";

import { StaffPageShell } from "../staff-page-shell";
import { ImportsPicker } from "./imports-picker";

export default async function ImportsPage() {
  return (
    <StaffPageShell activePath="/imports">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader
          title="Import sales"
          description="Choose an Event to record off-platform sales from a .csv or .xlsx file."
        />
        <ImportsPicker />
      </div>
    </StaffPageShell>
  );
}
