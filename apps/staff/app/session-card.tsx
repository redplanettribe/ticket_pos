"use client";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { useRoleName } from "@/app/role-name";

export type SessionCardProps = {
  email: string | null;
  organizationName: string | null;
  role: string | null;
};

/**
 * The Staff Session as the dashboard reads it back: who is signed in, which
 * Organization they are acting for, and as what.
 *
 * A client component for one word. The role has to be drawn through
 * `useRoleName` — that module is the only place in the app allowed to turn a
 * role token into a Spanish word (messages/README.md), and it is a hook, so the
 * card that renders one cannot be a Server Component. The alternative was a
 * second copy of the token-to-key map on this page, which is precisely the
 * "three Spanish words for Event Staff" that `app/role-name.ts` exists to
 * prevent. The session itself is still loaded on the server and handed down.
 *
 * Its words come from `shell` rather than a namespace of its own: an email
 * address, the active Organization and a role are the vocabulary the panel, the
 * organization switcher and the sign-out control already speak, and this page
 * says nothing else.
 */
export function SessionCard({ email, organizationName, role }: SessionCardProps) {
  const t = useTranslations("shell");
  const roleName = useRoleName();

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("sessionTitle")}</CardTitle>
        <CardDescription>{t("sessionDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid gap-4 sm:grid-cols-2">
          <div>
            <dt className="text-sm font-medium text-muted-foreground">{t("sessionEmail")}</dt>
            {/* An email address is data, and reads as written in both languages. */}
            <dd className="mt-1">{email ?? t("sessionEmailUnknown")}</dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-muted-foreground">
              {t("sessionOrganization")}
            </dt>
            <dd className="mt-1">{organizationName ?? t("sessionNoOrganization")}</dd>
          </div>
          {role ? (
            <div>
              <dt className="text-sm font-medium text-muted-foreground">{t("sessionRole")}</dt>
              <dd className="mt-1">{roleName(role)}</dd>
            </div>
          ) : null}
        </dl>
      </CardContent>
    </Card>
  );
}
