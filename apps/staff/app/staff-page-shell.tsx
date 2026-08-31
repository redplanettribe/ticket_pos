import type { ReactNode } from "react";
import { cache } from "react";

import type { OperatorShellLabels, StaffShellLabels } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";
import { cookies } from "next/headers";

import { callBackend } from "@/lib/api";
import { SESSION_COOKIE_NAME } from "@/lib/session";
import { loadSession, type SessionData } from "@/lib/staff-session";

import { LogoutButton } from "./logout-button";
import { ShellWithOrganizationSwitcher } from "./organization-switcher-dialog";
import { ShellLanguageSwitcher } from "./shell-language-switcher";

/**
 * The session, and the shape of it, now live in lib/staff-session.ts — i18n's
 * request configuration needs them too and cannot import a module that pulls in
 * client components. Re-exported here because three dozen pages import them from
 * this file, and moving a type is not worth touching all of them.
 */
export { loadSession };
export type { SessionData };

/**
 * How many organizations are waiting to be paid, platform-wide — worn by the
 * organization switcher control on an Organization's panel, and by the Payout
 * Requests entry once an operator has crossed over (#176, #192, ADR 0026).
 *
 * A queue's whole value is being noticed by somebody who had not already decided
 * to look, so this is read on every staff page an operator opens rather than
 * only on the queue itself. It is a count over a partial index, which is why
 * that is affordable.
 *
 * A failure returns null and the badge simply does not render. A navigation
 * element is not worth an error page, and the queue is one click away either
 * way.
 */
const loadPendingPayoutRequestCount = cache(async (): Promise<number | null> => {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  if (!token) {
    return null;
  }

  try {
    const envelope = await callBackend<{ pending_count: number }>(
      "/api/v1/operator/payout-requests/count",
      { method: "GET", sessionToken: token },
    );
    return envelope.data?.pending_count ?? null;
  } catch {
    return null;
  }
});

type StaffPageShellProps = {
  activePath: string;
  children: ReactNode;
};

/**
 * The panels' words, resolved once on the server and handed down.
 *
 * @ticket-pos/ui is shared with the Storefront, whose catalog is deliberately a
 * different one (ADR 0041), so the shells there take their copy as props rather
 * than reaching for a `t` of their own. This function is where the staff catalog
 * meets them, and it is a single object per shell so that a nav key added to
 * @ticket-pos/ui is a type error HERE — at the one place that can translate it —
 * rather than a blank row in a panel.
 *
 * Both panels' words come out of the `shell` namespace, the Operator Dashboard's
 * included. The Operator Dashboard has surfaces of its own, which `operator`
 * speaks for, but the panel around them is chrome, and chrome is what `shell`
 * speaks for.
 */
async function shellLabels(
  organizationName: string,
): Promise<{ staff: StaffShellLabels; operator: OperatorShellLabels }> {
  const t = await getTranslations("shell");
  const sidebar = {
    skipToContent: t("skipToContent"),
    primaryNavigation: t("primaryNavigation"),
    navigationMenu: t("navigationMenu"),
    openNavigationMenu: t("openNavigationMenu"),
    closeNavigationMenu: t("closeNavigationMenu"),
  };

  return {
    staff: {
      organizationHeading: t("organizationHeading"),
      organizationLogoAlt: t("organizationLogoAlt", { organization: organizationName }),
      nav: {
        dashboard: t("navDashboard"),
        events: t("navEvents"),
        payouts: t("navPayouts"),
        settings: t("navSettings"),
      },
      sidebar,
    },
    operator: {
      operatorHeading: t("operatorHeading"),
      platform: t("platform"),
      nav: {
        overview: t("navOverview"),
        organizations: t("navOrganizations"),
        payoutRequests: t("navPayoutRequests"),
        findSale: t("navFindSale"),
        customerConsent: t("navCustomerConsent"),
        taxInvoicing: t("navTaxInvoicing"),
        legalCenter: t("navLegalCenter"),
      },
      sidebar,
    },
  };
}

export async function StaffPageShell({ activePath, children }: StaffPageShellProps) {
  const session = await loadSession();
  // The product's own name, and therefore not copy: it reads as coined in both
  // languages, the same rule an Organization's name follows.
  const organizationName = session?.active_member?.organization_name ?? "Multiticketing";
  // After the session rather than beside it, because the logo's alt text names
  // the Organization. loadSession is cache()d and already resolved by the time
  // this runs, so the wait costs nothing.
  const labels = await shellLabels(organizationName);
  const organizationLogoUrl = session?.active_member?.organization_logo_url ?? null;
  // Asked for only when the switcher will render it. A non-operator would be
  // refused by the API anyway; not asking is the cheaper way to say the same.
  const pendingPayoutRequests = session?.is_platform_operator
    ? await loadPendingPayoutRequestCount()
    : null;
  // Payouts and Settings are both an Org Admin's, for reasons of their own: a
  // Payout Request is theirs to make, the Organization's settings theirs to
  // keep. Two answers that agree today, asked separately so a change to one
  // cannot quietly move the other (#190).
  const isOrgAdmin = session?.active_member?.role === "org_admin";

  return (
    <ShellWithOrganizationSwitcher
      staffLabels={labels.staff}
      operatorLabels={labels.operator}
      organizationName={organizationName}
      organizationLogoUrl={organizationLogoUrl}
      activePath={activePath}
      showSettings={isOrgAdmin}
      showPayouts={isOrgAdmin}
      showEvents={session?.active_member != null}
      isPlatformOperator={session?.is_platform_operator === true}
      pendingPayoutRequests={pendingPayoutRequests}
      memberships={session?.memberships ?? []}
      activeMemberId={session?.active_member?.member_id}
      /*
        The two personal controls, in the neighbourhood ADR 0041 puts them in: at
        the foot of whichever panel is drawn, which is the one place reachable by
        an Event Staff member with no administrative rights AND by a Platform
        Operator who belongs to no Organization.
      */
      userMenu={
        <div className="space-y-1">
          <ShellLanguageSwitcher />
          <LogoutButton />
        </div>
      }
    >
      {children}
    </ShellWithOrganizationSwitcher>
  );
}
