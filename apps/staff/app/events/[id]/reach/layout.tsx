import type { ReactNode } from "react";

import { redirect } from "next/navigation";
import { getTranslations } from "next-intl/server";

import { reachNavItems, type ReachNavKey } from "@ticket-pos/ui";

import { loadSession } from "../../../staff-page-shell";
import { ReachTabs } from "./reach-tabs";

type EventReachLayoutProps = {
  params: Promise<{ id: string }>;
  children: ReactNode;
};

/**
 * The Reach surface of an Event (#464): how its page was reached, and the
 * Affiliate Links it is reached with. A small surface of its own with two
 * sub-tabs — Reach Trends first, the links beneath — in the mould of the Sales
 * surface, and grown out of the old Affiliate Links tab, which stacked the same
 * two things on one page in the other order.
 *
 * Top to bottom it owns what belongs to both sub-tabs alike: the guard, and the
 * tab strip. Nothing sits above the strip — a summary strip was considered and
 * refused, because the one figure it could honestly carry (page views since
 * counting began) does not earn a strip, and a Clicks figure there would
 * contradict the table's authoritative lifetime count (ADR 0057).
 *
 * The tabs are routes and not client state, so the chart can be bookmarked and
 * a colleague sent straight to the links, and a visit loads only the one being
 * read.
 */
export default async function EventReachLayout({ params, children }: EventReachLayoutProps) {
  const { id } = await params;
  const [session, t] = await Promise.all([loadSession(), getTranslations("reach")]);
  const role = session?.active_member?.role;

  // The whole surface is Org Admin / Event Owner, the gate Affiliate Links has
  // always carried: the Event panel hides the entry from Event Staff, and this
  // guard is for the URL somebody was sent — the API refuses them regardless.
  // Once at the layout rather than once per sub-tab, because the two tabs share
  // the gate and a strip drawn over a page about to redirect helps nobody.
  if (role !== "org_admin" && role !== "event_owner") {
    redirect(`/events/${id}/ticket-types`);
  }

  /*
    The strip's words, resolved here and handed down. `reachNavItems` returns
    keys and never words, because @ticket-pos/ui is shared with the Storefront
    and cannot reach this catalog (ADR 0041) — this is the one place that can
    turn them into a language. A Record rather than a lookup per entry so that a
    tab added to `REACH_NAV_KEYS` becomes a compile error here.
  */
  const labels: Record<ReachNavKey, string> = {
    trends: t("tabTrends"),
    affiliateLinks: t("tabAffiliateLinks"),
  };

  const tabs = reachNavItems({ eventId: id }).map((entry) => ({
    ...entry,
    label: labels[entry.key],
  }));

  // Always drawn: both tabs are for the same two roles, so once the guard above
  // has let somebody in there are always two on offer — never the one-tab strip
  // the Sales layout has to suppress for Event Staff.
  return (
    <div className="space-y-6">
      <ReachTabs items={tabs} ariaLabel={t("tabsLabel")} />
      {children}
    </div>
  );
}
