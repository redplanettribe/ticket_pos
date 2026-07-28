"use client";

import { Badge, Button, Card, CardContent, cn, OrgAvatar } from "@ticket-pos/ui";

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

function MembershipDetails({ membership }: { membership: Membership }) {
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

type MembershipListBaseProps = {
  memberships: Membership[];
  activeMemberId?: string;
  disabled?: boolean;
};

type GateModeProps = MembershipListBaseProps & {
  mode: "gate";
  onSelect: (memberId: string) => void;
  selecting?: string | null;
};

type SwitcherModeProps = MembershipListBaseProps & {
  mode: "switcher";
  onSelect: (memberId: string) => void;
  switching?: string | null;
};

export type MembershipListProps = GateModeProps | SwitcherModeProps;

export function MembershipList(props: MembershipListProps) {
  const { memberships, mode, activeMemberId, disabled = false } = props;

  if (memberships.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No organizations found for your account.</p>
    );
  }

  return (
    <ul className="space-y-3">
      {memberships.map((membership) => {
        const isActive = activeMemberId === membership.member_id;
        const isBusy =
          mode === "gate"
            ? props.selecting === membership.member_id
            : props.switching === membership.member_id;

        if (mode === "gate") {
          return (
            <li key={membership.member_id}>
              <Card>
                <CardContent className="flex items-center justify-between gap-4 p-4">
                  <MembershipDetails membership={membership} />
                  <Button
                    type="button"
                    onClick={() => props.onSelect(membership.member_id)}
                    disabled={disabled || props.selecting !== null}
                    aria-busy={isBusy}
                  >
                    {isBusy ? "Selecting..." : "Select"}
                  </Button>
                </CardContent>
              </Card>
            </li>
          );
        }

        return (
          <li key={membership.member_id}>
            <button
              type="button"
              className={cn(
                "w-full rounded-lg text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                !isActive && !disabled && props.switching === null && "cursor-pointer",
              )}
              onClick={() => {
                if (!isActive) {
                  props.onSelect(membership.member_id);
                }
              }}
              disabled={disabled || isActive || props.switching !== null}
              aria-busy={isBusy}
              aria-current={isActive ? "true" : undefined}
            >
              <Card
                className={cn(
                  "transition-colors",
                  isActive
                    ? "border-primary bg-primary/5"
                    : "hover:bg-accent/50",
                )}
              >
                <CardContent className="flex items-center justify-between gap-4 p-4">
                  <MembershipDetails membership={membership} />
                  {isActive ? (
                    <Badge variant="secondary">Current</Badge>
                  ) : isBusy ? (
                    <span className="text-sm text-muted-foreground">Switching...</span>
                  ) : null}
                </CardContent>
              </Card>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
