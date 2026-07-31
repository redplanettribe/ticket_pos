"use client";

import { Button, Card, CardContent, OrgAvatar } from "@ticket-pos/ui";

export type Membership = {
  member_id: string;
  organization_id: string;
  organization_name: string;
  organization_slug: string;
  organization_logo_url: string | null;
  role: string;
};

function formatRole(role: string): string {
  return role.replace("_", " ");
}

/**
 * An Organization as it reads in a list of them: mark, name, and the slug and
 * role that tell two similarly named ones apart. Shared with the organization
 * switcher, where an Organization sits beside the Platform entry (#191).
 */
export function MembershipDetails({ membership }: { membership: Membership }) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <OrgAvatar logoUrl={membership.organization_logo_url} name={membership.organization_name} shape="tile" />
      <div className="min-w-0">
        <p className="font-medium">{membership.organization_name}</p>
        <p className="text-sm text-muted-foreground">
          {membership.organization_slug} · {formatRole(membership.role)}
        </p>
      </div>
    </div>
  );
}

export type MembershipListProps = {
  memberships: Membership[];
  activeMemberId?: string;
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
  const { memberships, activeMemberId, disabled = false } = props;

  if (memberships.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No organizations found for your account.</p>
    );
  }

  return (
    <ul className="space-y-3">
      {memberships.map((membership) => {
        const isActive = activeMemberId === membership.member_id;
        const isBusy = props.selecting === membership.member_id;

        return (
          <li key={membership.member_id}>
            <Card>
              <CardContent className="flex items-center justify-between gap-4 p-4">
                <MembershipDetails membership={membership} />
                <Button
                  type="button"
                  onClick={() => props.onSelect(membership.member_id)}
                  disabled={disabled || isActive || props.selecting != null}
                  aria-busy={isBusy}
                >
                  {isBusy ? "Selecting..." : "Select"}
                </Button>
              </CardContent>
            </Card>
          </li>
        );
      })}
    </ul>
  );
}
