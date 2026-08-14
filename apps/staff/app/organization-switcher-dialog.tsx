"use client";

import {
  Alert,
  AlertDescription,
  Badge,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  OperatorShell,
  PlatformMark,
  StaffShell,
  cn,
  isOnOperatorSurface,
  type OperatorShellLabels,
  type StaffShellLabels,
} from "@ticket-pos/ui";
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";

import { MembershipDetails, type Membership } from "@/app/membership-list";
import { apiErrorMessage } from "@/lib/api-errors";
import { formatNumber } from "@/lib/format";
import { switcherEntries, visiblePendingPayoutRequestCount } from "@/lib/organization-switcher";

/**
 * How many organizations are waiting to be paid (#176, ADR 0026). Worn by the
 * switcher control so an operator working inside an Organization still sees
 * that requests have queued up, repeated on the Platform entry so the opened
 * switcher says what it is offering, and worn by the Payout Requests nav entry
 * once they have crossed over (#192).
 *
 * The count goes through lib/format so it wears the reader's group mark rather
 * than their machine's — the shell's own instance of the bug ADR 0041 records,
 * where staff numbers followed the browser and nothing chose. The sentence a
 * screen reader hears is the catalog's, pluralized by ICU, because "1 payout
 * request" and "3 payout requests" do not inflect the same way in Spanish as in
 * English and gluing a number to a noun is what the catalog exists to prevent.
 */
function PendingPayoutRequestsBadge({ count }: { count: number }) {
  const t = useTranslations("shell");
  const locale = toAppLocale(useLocale());

  return (
    <span
      className="rounded-full bg-primary px-2 py-0.5 text-xs font-semibold tabular-nums text-primary-foreground"
      aria-label={t("pendingPayoutRequests", { count })}
    >
      {formatNumber(count, locale)}
    </span>
  );
}

/** One choice in the switcher, drawn the same whichever hat it offers. */
function SwitcherEntryButton({
  currentLabel,
  onSelect,
  isCurrent,
  disabled,
  busyLabel,
  details,
  trailing,
}: {
  currentLabel: string;
  onSelect: () => void;
  isCurrent: boolean;
  disabled: boolean;
  busyLabel: string | null;
  details: ReactNode;
  trailing?: ReactNode;
}) {
  return (
    <li>
      <button
        type="button"
        className={cn(
          "w-full rounded-lg text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
          !isCurrent && !disabled && "cursor-pointer",
        )}
        onClick={() => {
          if (!isCurrent) {
            onSelect();
          }
        }}
        disabled={disabled || isCurrent}
        aria-busy={busyLabel !== null}
        aria-current={isCurrent ? "true" : undefined}
      >
        <Card
          className={cn("transition-colors", isCurrent ? "border-primary bg-primary/5" : "hover:bg-accent/50")}
        >
          <CardContent className="flex items-center justify-between gap-4 p-4">
            {details}
            {isCurrent ? (
              <Badge variant="secondary">{currentLabel}</Badge>
            ) : busyLabel ? (
              <span className="text-sm text-muted-foreground">{busyLabel}</span>
            ) : (
              trailing
            )}
          </CardContent>
        </Card>
      </button>
    </li>
  );
}

/**
 * The Platform entry as it reads beside the Organizations: not one more of
 * them, but the other hat — an authority spanning every Organization
 * (CONTEXT.md).
 */
function PlatformDetails() {
  const t = useTranslations("shell");

  return (
    <div className="flex min-w-0 items-center gap-3">
      <PlatformMark />
      <div className="min-w-0">
        <p className="font-medium">{t("platform")}</p>
        <p className="text-sm text-muted-foreground">{t("platformDescription")}</p>
      </div>
    </div>
  );
}

type OrganizationSwitcherDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memberships: Membership[];
  activeMemberId?: string;
  /** Whether this session may act for the platform at all (ADR 0015). */
  isPlatformOperator: boolean;
  /** True while the Operator Dashboard is the surface being looked at. */
  onPlatform: boolean;
  pendingPayoutRequests: number | null;
};

export function OrganizationSwitcherDialog({
  open,
  onOpenChange,
  memberships,
  activeMemberId,
  isPlatformOperator,
  onPlatform,
  pendingPayoutRequests,
}: OrganizationSwitcherDialogProps) {
  const t = useTranslations("shell");
  // The `errors` namespace as plain data, which is what lib/api-errors resolves
  // against. Handed in rather than imported so that module stays free of
  // next-intl and testable under `node --test` (ADR 0023).
  const errorCopy = useMessages().errors;
  const router = useRouter();
  const [switchingMemberId, setSwitchingMemberId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const entries = switcherEntries({ memberships, isPlatformOperator });
  const waiting = visiblePendingPayoutRequestCount(pendingPayoutRequests);

  async function handleSwitch(memberId: string) {
    setSwitchingMemberId(memberId);
    setError(null);

    try {
      const response = await fetch("/api/auth/select-organization", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ member_id: memberId }),
      });
      const envelope = (await response.json()) as {
        error: { code: string; message: string; details?: unknown } | null;
      };
      if (!response.ok || envelope.error) {
        // The API's code first, its English message as the floor beneath a
        // failure this app has no words for, and this surface's own sentence
        // only when there was nothing to read at all (ADR 0023).
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("switchFailed"));
        return;
      }
      onOpenChange(false);
      router.push("/");
      router.refresh();
    } catch {
      // Never reached the API: there is no verdict to render, only ours.
      setError(t("switchFailed"));
    } finally {
      setSwitchingMemberId(null);
    }
  }

  /*
    Changing to Platform changes no server-side state: operator authority is not
    a Staff Session's active Membership but something the session either has or
    has not (CONTEXT.md), so the switch is a navigation and the Organization the
    session was acting for is still there when they switch back.
  */
  function handleSwitchToPlatform() {
    onOpenChange(false);
    router.push("/operator");
  }

  function handleCreateOrganization() {
    onOpenChange(false);
    router.push("/organizations/new");
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("switchOrganization")}</DialogTitle>
          <DialogDescription>
            {isPlatformOperator
              ? t("switchOrganizationOperatorDescription")
              : t("switchOrganizationDescription")}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        {entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("noOrganizations")}</p>
        ) : (
          <ul className="space-y-3">
            {entries.map((entry) => {
              if (entry.kind === "platform") {
                return (
                  <SwitcherEntryButton
                    key="platform"
                    currentLabel={t("current")}
                    onSelect={handleSwitchToPlatform}
                    isCurrent={onPlatform}
                    disabled={switchingMemberId !== null}
                    busyLabel={null}
                    details={<PlatformDetails />}
                    trailing={waiting ? <PendingPayoutRequestsBadge count={waiting} /> : null}
                  />
                );
              }

              const { membership } = entry;
              // On the Operator Dashboard no Organization is the one being
              // acted for, however the session's active Membership reads.
              const isCurrent = !onPlatform && activeMemberId === membership.member_id;

              return (
                <SwitcherEntryButton
                  key={membership.member_id}
                  currentLabel={t("current")}
                  onSelect={() => void handleSwitch(membership.member_id)}
                  isCurrent={isCurrent}
                  disabled={switchingMemberId !== null}
                  busyLabel={switchingMemberId === membership.member_id ? t("switching") : null}
                  details={<MembershipDetails membership={membership} />}
                />
              );
            })}
          </ul>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleCreateOrganization}>
            {t("createOrganization")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type ShellWithOrganizationSwitcherProps = {
  /**
   * The panels' words, resolved on the server by StaffPageShell.
   *
   * Handed down rather than looked up here because the panels live in
   * @ticket-pos/ui, which is shared with the Storefront and so cannot reach the
   * staff catalog. Everything this file draws ITSELF reads the catalog directly
   * through `useTranslations` — the provider is mounted app-wide.
   */
  staffLabels: StaffShellLabels;
  operatorLabels: OperatorShellLabels;
  organizationName: string;
  organizationLogoUrl?: string | null;
  activePath: string;
  showSettings: boolean;
  showPayouts: boolean;
  showEvents: boolean;
  isPlatformOperator: boolean;
  /** How many payout requests are waiting platform-wide; null when unknown or not an operator. */
  pendingPayoutRequests?: number | null;
  memberships: Membership[];
  activeMemberId?: string;
  userMenu: ReactNode;
  children: ReactNode;
};

/**
 * The staff app's side panel, whichever hat is being worn, with the switcher
 * that changes hats wired to its header.
 *
 * Which panel is a consequence of where the user is, not of anything the caller
 * decides: inside the operator surface the Organization's panel is a category
 * error — its Dashboard, Events, Payouts and Settings are not what a
 * Platform Operator is doing — so the Operator Dashboard's own panel replaces
 * it outright (#192).
 */
export function ShellWithOrganizationSwitcher({
  staffLabels,
  operatorLabels,
  organizationName,
  organizationLogoUrl,
  activePath,
  showSettings,
  showPayouts,
  showEvents,
  isPlatformOperator,
  pendingPayoutRequests = null,
  memberships,
  activeMemberId,
  userMenu,
  children,
}: ShellWithOrganizationSwitcherProps) {
  const [open, setOpen] = useState(false);

  const onPlatform = isOnOperatorSurface(activePath);
  const waiting = visiblePendingPayoutRequestCount(pendingPayoutRequests);
  const badge = waiting ? <PendingPayoutRequestsBadge count={waiting} /> : null;

  return (
    <>
      {onPlatform ? (
        /*
          Inside the operator surface the queue has a page of its own in view, so
          the count belongs on that entry and not on the switcher control above
          it: the same number twice, one line apart, says nothing the first
          saying did not. On an Organization's panel there is no such entry, and
          the control keeps wearing it (#191).
        */
        <OperatorShell
          labels={operatorLabels}
          activePath={activePath}
          payoutRequestBadge={badge}
          userMenu={userMenu}
          onPlatformClick={() => setOpen(true)}
        >
          {children}
        </OperatorShell>
      ) : (
        <StaffShell
          labels={staffLabels}
          organizationName={organizationName}
          organizationLogoUrl={organizationLogoUrl}
          activePath={activePath}
          showSettings={showSettings}
          showPayouts={showPayouts}
          showEvents={showEvents}
          organizationBadge={badge}
          userMenu={userMenu}
          onOrganizationClick={() => setOpen(true)}
        >
          {children}
        </StaffShell>
      )}
      <OrganizationSwitcherDialog
        open={open}
        onOpenChange={setOpen}
        memberships={memberships}
        activeMemberId={activeMemberId}
        isPlatformOperator={isPlatformOperator}
        onPlatform={onPlatform}
        pendingPayoutRequests={pendingPayoutRequests}
      />
    </>
  );
}
