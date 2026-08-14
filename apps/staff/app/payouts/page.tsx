import { PageHeader } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";
import { redirect } from "next/navigation";

import { StaffPageShell, loadSession } from "../staff-page-shell";

import { PayoutsPageClient } from "./payouts-page-client";

export default async function PayoutsPage() {
  const t = await getTranslations("payouts");
  const session = await loadSession();

  // Getting paid is an Org Admin's business: the ask is theirs to make and the
  // Payout Profile is read by them and by Platform Operators alone (CONTEXT.md).
  // The nav hides this entry from everyone else; guard the route too, since it
  // stays directly reachable by URL — and the API refuses them regardless.
  if (session?.active_member?.role !== "org_admin") {
    redirect("/");
  }

  return (
    <StaffPageShell activePath="/payouts">
      <div className="mx-auto max-w-4xl space-y-6">
        <PageHeader title={t("title")} description={t("description")} />
        <PayoutsPageClient />
      </div>
    </StaffPageShell>
  );
}
