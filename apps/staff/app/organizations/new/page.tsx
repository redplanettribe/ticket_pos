import Link from "next/link";
import { getTranslations } from "next-intl/server";

import { Button, PageHeader } from "@ticket-pos/ui";

import { CreateOrganizationForm } from "@/app/create-organization-form";
import { CreateOrganizationGate } from "@/app/create-organization-gate";
import { loadSession, StaffPageShell } from "@/app/staff-page-shell";

export default async function NewOrganizationPage() {
  const session = await loadSession();
  const t = await getTranslations("onboarding");

  if (session?.active_member) {
    return (
      <StaffPageShell activePath="/">
        <div className="mx-auto max-w-lg space-y-6">
          <PageHeader
            title={t("createTitle")}
            description={t("createDescription")}
            actions={
              <Button variant="outline" asChild>
                <Link href="/">{t("cancel")}</Link>
              </Button>
            }
          />
          <CreateOrganizationForm />
        </div>
      </StaffPageShell>
    );
  }

  return <CreateOrganizationGate />;
}
