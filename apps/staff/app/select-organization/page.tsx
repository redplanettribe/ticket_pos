import Link from "next/link";

import { Button } from "@ticket-pos/ui";

import { LogoutButton } from "@/app/logout-button";
import { OrganizationPicker } from "@/app/organization-picker";

export default function SelectOrganizationPage() {
  return (
    <OrganizationPicker
      title="Select organization"
      description="Choose which organization you want to work in."
      footer={<LogoutButton />}
      actions={
        <Button variant="outline" className="w-full" asChild>
          <Link href="/organizations/new">Create organization</Link>
        </Button>
      }
    />
  );
}
