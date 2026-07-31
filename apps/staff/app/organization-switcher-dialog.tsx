"use client";

import {
  Alert,
  AlertDescription,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  StaffShell,
  cn,
  isNavItemActive,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";

import { MembershipList, type Membership } from "@/app/membership-list";

type OrganizationSwitcherDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memberships: Membership[];
  activeMemberId?: string;
};

export function OrganizationSwitcherDialog({
  open,
  onOpenChange,
  memberships,
  activeMemberId,
}: OrganizationSwitcherDialogProps) {
  const router = useRouter();
  const [switchingMemberId, setSwitchingMemberId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleSwitch(memberId: string) {
    if (memberId === activeMemberId) {
      return;
    }

    setSwitchingMemberId(memberId);
    setError(null);

    try {
      const response = await fetch("/api/auth/select-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ member_id: memberId }),
      });
      const envelope = (await response.json()) as {
        error: { message: string } | null;
      };
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not switch organization");
        return;
      }
      onOpenChange(false);
      router.push("/");
      router.refresh();
    } catch {
      setError("Could not switch organization");
    } finally {
      setSwitchingMemberId(null);
    }
  }

  function handleCreateOrganization() {
    onOpenChange(false);
    router.push("/organizations/new");
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Switch organization</DialogTitle>
          <DialogDescription>Select an organization to work in, or create a new one.</DialogDescription>
        </DialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <MembershipList
          mode="switcher"
          memberships={memberships}
          activeMemberId={activeMemberId}
          switching={switchingMemberId}
          disabled={switchingMemberId !== null}
          onSelect={(memberId) => void handleSwitch(memberId)}
        />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleCreateOrganization}>
            Create organization
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * The Operator Dashboard entry. StaffShell's primary navigation is a fixed list
 * owned by the shared UI package, so this platform-wide entry rides the shell's
 * sidebar footer slot above the user menu — visually a nav item, deliberately
 * set apart from the Organization-scoped links it does not belong with. It is
 * rendered only for a session on the platform operator allowlist (ADR 0015).
 */
function OperatorNavLink({
  active,
  pendingPayoutRequests,
}: {
  active: boolean;
  pendingPayoutRequests: number | null;
}) {
  return (
    <a
      href="/operator"
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center justify-between gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
        active ? "bg-accent text-accent-foreground" : "text-muted-foreground",
      )}
    >
      <span>Operator</span>
      {/*
        How many organizations are waiting to be paid (#176, ADR 0026). A queue's
        whole value is being noticed by somebody who had not already decided to
        look — a Friday-evening request otherwise waits until an operator happens
        to click. Absent at zero rather than shown as a "0": a badge saying
        nothing is waiting is a badge that trains its reader to ignore it. Null
        means the count could not be read, which is also nothing to show.
      */}
      {pendingPayoutRequests ? (
        <span
          className="rounded-full bg-primary px-2 py-0.5 text-xs font-semibold tabular-nums text-primary-foreground"
          aria-label={`${pendingPayoutRequests} payout requests waiting`}
        >
          {pendingPayoutRequests}
        </span>
      ) : null}
    </a>
  );
}

type StaffShellWithOrganizationSwitcherProps = {
  organizationName: string;
  organizationLogoUrl?: string | null;
  activePath: string;
  showSettings: boolean;
  showEvents: boolean;
  showOperator: boolean;
  /** How many payout requests are waiting platform-wide; null when unknown or not an operator. */
  pendingPayoutRequests?: number | null;
  memberships: Membership[];
  activeMemberId?: string;
  userMenu: ReactNode;
  children: ReactNode;
};

export function StaffShellWithOrganizationSwitcher({
  organizationName,
  organizationLogoUrl,
  activePath,
  showSettings,
  showEvents,
  showOperator,
  pendingPayoutRequests = null,
  memberships,
  activeMemberId,
  userMenu,
  children,
}: StaffShellWithOrganizationSwitcherProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <StaffShell
        organizationName={organizationName}
        organizationLogoUrl={organizationLogoUrl}
        activePath={activePath}
        showSettings={showSettings}
        showEvents={showEvents}
        userMenu={
          showOperator ? (
            <div className="space-y-1">
              <OperatorNavLink
                active={isNavItemActive(activePath, "/operator")}
                pendingPayoutRequests={pendingPayoutRequests}
              />
              {userMenu}
            </div>
          ) : (
            userMenu
          )
        }
        onOrganizationClick={() => setOpen(true)}
      >
        {children}
      </StaffShell>
      <OrganizationSwitcherDialog
        open={open}
        onOpenChange={setOpen}
        memberships={memberships}
        activeMemberId={activeMemberId}
      />
    </>
  );
}
