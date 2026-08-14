import { PageHeader } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { StaffPageShell } from "../staff-page-shell";

import { SettingsPageClient } from "./settings-page-client";

export default async function SettingsPage() {
  const t = await getTranslations("organization");

  return (
    <StaffPageShell activePath="/settings">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader title={t("title")} description={t("description")} />
        <SettingsPageClient />
      </div>
    </StaffPageShell>
  );
}
