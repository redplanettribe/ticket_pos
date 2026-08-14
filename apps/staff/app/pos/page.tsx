import { InProgressPanel, PageHeader } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { StaffPageShell } from "../staff-page-shell";

/**
 * The point of sale.
 *
 * Of every surface in this application this is the one an English-only interface
 * costs the most: it is read at a door, under time pressure, on a phone, and
 * frequently by an Event Staff member who was hired for the night rather than by
 * the organizer who set the Event up in the first place. Whoever builds the
 * selling flow out from this placeholder inherits that: every sentence on it
 * comes from the `pos` namespace, and none of it is written in a component.
 */
export default async function POSPage() {
  const t = await getTranslations("pos");

  return (
    <StaffPageShell activePath="/pos">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader title={t("title")} description={t("description")} />
        <InProgressPanel title={t("inProgressTitle")} description={t("inProgressDescription")} />
      </div>
    </StaffPageShell>
  );
}
