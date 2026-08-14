"use client";

import { Button, Card, CardContent, OrgAvatar } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { useRoleName } from "@/app/role-name";

export type Membership = {
  member_id: string;
  organization_id: string;
  organization_name: string;
  organization_slug: string;
  organization_logo_url: string | null;
  role: string;
};

/**
 * An Organization as it reads in a list of them: mark, name, and the slug and
 * role that tell two similarly named ones apart. Shared with the organization
 * switcher, where an Organization sits beside the Platform entry (#191).
 */
export function MembershipDetails({ membership }: { membership: Membership }) {
  const roleName = useRoleName();
  const t = useTranslations("shell");

  return (
    <div className="flex min-w-0 items-center gap-3">
      <OrgAvatar
        logoUrl={membership.organization_logo_url}
        name={membership.organization_name}
        alt={t("organizationLogoAlt", { organization: membership.organization_name })}
        shape="tile"
      />
      <div className="min-w-0">
        <p className="font-medium">{membership.organization_name}</p>
        {/*
          The slug and the role, joined by a separator that is ours rather than
          either language's. The Organization's own name and slug are data and
          read as coined in both languages (messages/README.md).
        */}
        <p className="text-sm text-muted-foreground">
          {membership.organization_slug} · {roleName(membership.role)}
        </p>
      </div>
    </div>
  );
}

export type MembershipListProps = {
  memberships: Membership[];
  disabled?: boolean;
  onSelect: (memberId: string) => void;
  selecting?: string | null;
};

/**
 * The gate a staff user passes on the way in, choosing which Organization this
 * Staff Session acts for. Switching later is the organization switcher's job,
 * and that draws its own list because the Platform entry belongs there too
 * (#191) — an entry that is no Membership.
 */
export function MembershipList(props: MembershipListProps) {
  const t = useTranslations("shell");
  const { memberships, disabled = false } = props;

  if (memberships.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("noOrganizations")}</p>;
  }

  return (
    <ul className="space-y-3">
      {memberships.map((membership) => {
        const isBusy = props.selecting === membership.member_id;

        return (
          <li key={membership.member_id}>
            <Card>
              <CardContent className="flex items-center justify-between gap-4 p-4">
                <MembershipDetails membership={membership} />
                <Button
                  type="button"
                  onClick={() => props.onSelect(membership.member_id)}
                  disabled={disabled || props.selecting != null}
                  aria-busy={isBusy}
                >
                  {isBusy ? t("selecting") : t("select")}
                </Button>
              </CardContent>
            </Card>
          </li>
        );
      })}
    </ul>
  );
}
