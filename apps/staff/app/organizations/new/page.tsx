import Link from "next/link";

import { Button, PageHeader } from "@ticket-pos/ui";

import { CreateOrganizationForm } from "@/app/create-organization-form";
import { CreateOrganizationGate } from "@/app/create-organization-gate";
import { loadSession, StaffPageShell } from "@/app/staff-page-shell";

export default async function NewOrganizationPage() {
  const session = await loadSession();

  if (session?.active_member) {
    return (
      <StaffPageShell activePath="/">
        <div className="mx-auto max-w-lg space-y-6">
          <PageHeader
            title="Create organization"
            description="Name your venue and choose a URL slug for your storefront."
            actions={
              <Button variant="outline" asChild>
                <Link href="/">Cancel</Link>
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
