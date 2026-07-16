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

type StaffShellWithOrganizationSwitcherProps = {
  organizationName: string;
  organizationLogoUrl?: string | null;
  activePath: string;
  showSettings: boolean;
  showEvents: boolean;
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
        userMenu={userMenu}
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
