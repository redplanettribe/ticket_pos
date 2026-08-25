import type { ReactNode } from "react";

import { getTranslations } from "next-intl/server";

import { salesNavItems, shouldDrawTabStrip, type SalesNavKey } from "@ticket-pos/ui";

import { loadSession } from "../../../staff-page-shell";
import { SalesRefreshProvider } from "../sales-refresh";
import { SalesSummaryStrip } from "../sales-summary-strip";
import { SalesTabs } from "./sales-tabs";

type EventSalesLayoutProps = {
  params: Promise<{ id: string }>;
  children: ReactNode;
};

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
  };

  const entries = salesNavItems({ eventId: id, fullAccess });
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
