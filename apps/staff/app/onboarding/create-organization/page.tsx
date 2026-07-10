import { redirect } from "next/navigation";

export default function OnboardingCreateOrganizationPage() {
  redirect("/organizations/new");
}
