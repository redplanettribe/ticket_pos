import Link from "next/link";

import { Button } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { LogoutButton } from "@/app/logout-button";
import { OrganizationPicker } from "@/app/organization-picker";
import { ShellLanguageSwitcher } from "@/app/shell-language-switcher";

export default async function SelectOrganizationPage() {
  const t = await getTranslations("shell");

  return (
    <OrganizationPicker
      title={t("selectOrganization")}
      description={t("selectOrganizationDescription")}
      /*
        The same two personal controls the sidebar carries, because this surface
        has no sidebar: a Member who has not chosen an Organization yet is signed
        in, and must not be stranded in a language they cannot read while they
        decide. The language switcher here is the shell one — it writes the stored
        Staff Locale — because there IS a person now, unlike on /login.
      */
      footer={
        <div className="flex items-center justify-between gap-4">
          <ShellLanguageSwitcher />
          <LogoutButton />
        </div>
      }
      actions={
        <Button variant="outline" className="w-full" asChild>
          <Link href="/organizations/new">{t("createOrganization")}</Link>
        </Button>
      }
    />
  );
}
