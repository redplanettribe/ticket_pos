import type { ReactNode } from "react";

import { getTranslations } from "next-intl/server";
import { cookies } from "next/headers";

import { salesNavItems, shouldDrawTabStrip, type SalesNavKey } from "@ticket-pos/ui";

import { callBackend } from "@/lib/api";
import type { EventDetail } from "@/lib/events-api";
import { SESSION_COOKIE_NAME } from "@/lib/session";

import { loadSession } from "../../../staff-page-shell";
import { SalesRefreshProvider } from "../sales-refresh";
import { SalesSummaryStrip } from "../sales-summary-strip";
import { SalesTabs } from "./sales-tabs";

type EventSalesLayoutProps = {
  params: Promise<{ id: string }>;
  children: ReactNode;
};

/**
 * Whether the Event has a Holder List at all: the API serves it while EITHER
 * Ticket Assignment or Ticket Questions is open (#333), two features that ship
 * dark (ADR 0045).
 *
 * Read off the Event payload — the one answer to whether they are on, which is
 * why the staff app never reads an env var of its own. The Event layout above
 * fetches the same payload; `callBackend` goes through `fetch`, so within one
 * request that is one round-trip, not two. Tolerates failure as false: no
 * payload, no tab, which is the dark state the features ship in. Offering a tab
 * that answers 404 would be worse than not offering it, and while both features
 * are dark the tab must not exist at all — a nav entry is exactly the kind of
 * thing that admits a feature is there before the Privacy Policy describes it.
 */
async function fetchHolderListOpen(eventId: string, token: string): Promise<boolean> {
  try {
    const envelope = await callBackend<EventDetail>(`/api/v1/staff/events/${eventId}`, {
      method: "GET",
      sessionToken: token,
    });
    return Boolean(
      envelope.data?.ticket_assignment_enabled || envelope.data?.ticket_questions_enabled,
    );
  } catch {
    return false;
  }
}

/**
 * The Sales surface of an Event: a small surface of its own with sub-tabs.
 *
 * Top to bottom it owns the three things that belong to every sub-tab alike:
 *
 *  - the sales-refresh provider, so a Sale Import committed or undone on the
 *    Record tab still tells the Net Proceeds strip to re-read — the two used to
 *    be siblings on one page, and the provider has to outlive that;
 *  - the Net Proceeds strip, Org Admin and Event Owner only as it has always
 *    been, in view whichever sub-tab is open;
 *  - the tab strip itself, drawn only when there is more than one tab to offer.
 *
 * The Holder List (#469) is the last of those tabs, and the one whose gate is
 * not the surface's own: Org Admins alone, and only while the Event has a
 * roster to show. Both halves are established here — this is the one place
 * that holds the Event payload and the Member's role together — and handed to
 * `salesNavItems` as a single fact.
 *
 * The tabs are routes and not client state, so the list keeps its page, filters
 * and sort in the URL exactly as before, and a colleague can be sent straight to
 * the recording tools.
 */
export default async function EventSalesLayout({ params, children }: EventSalesLayoutProps) {
  const { id } = await params;
  const [session, t] = await Promise.all([loadSession(), getTranslations("sales")]);
  const role = session?.active_member?.role;

  // The list is for every Member of the Event; the Event's money and the tools
  // that record a sale are for the Org Admin and the Event Owner. The same line
  // the Sales page has always drawn, moved up one level.
  const fullAccess = role === "org_admin" || role === "event_owner";

  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE_NAME)?.value;
  const holderList =
    role === "org_admin" && Boolean(token) && (await fetchHolderListOpen(id, token as string));

  /*
    The strip's words, resolved here and handed down. `salesNavItems` returns
    keys and never words, because @ticket-pos/ui is shared with the Storefront
    and cannot reach this catalog (ADR 0041) — this is the one place that can
    turn them into a language. A Record rather than a lookup per entry so that a
    tab added to `SALES_NAV_KEYS` becomes a compile error here.
  */
  const labels: Record<SalesNavKey, string> = {
    sales: t("tabSales"),
    record: t("tabRecord"),
    trends: t("tabTrends"),
    holderList: t("tabHolderList"),
  };

  const entries = salesNavItems({ eventId: id, fullAccess, holderList });
  const tabs = entries.map((entry) => ({ ...entry, label: labels[entry.key] }));

  return (
    <SalesRefreshProvider>
      <div className="space-y-6">
        {fullAccess ? <SalesSummaryStrip eventId={id} /> : null}
        {/* Event Staff are offered the list alone, and a strip with one tab in
            it suggests a choice that is not on offer. */}
        {shouldDrawTabStrip(entries) ? (
          <SalesTabs items={tabs} ariaLabel={t("tabsLabel")} />
        ) : null}
        {children}
      </div>
    </SalesRefreshProvider>
  );
}
