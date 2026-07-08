import { LogoutButton } from "@/app/logout-button";
import { OrganizationPicker } from "@/app/organization-picker";

export default function SelectOrganizationPage() {
  return (
    <main>
      <OrganizationPicker
        title="Select organization"
        description="Choose which organization you want to work in."
      />
      <LogoutButton />
    </main>
  );
}
