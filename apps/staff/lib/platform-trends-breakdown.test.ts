import assert from "node:assert/strict";
import test from "node:test";

import {
  PLATFORM_TREND_BREAKDOWN_API_PATH,
  breakdownRequestPath,
  breakdownView,
  bucketSelectionIn,
  operatorOrganizationPath,
  type PlatformTrendBreakdown,
  type TrendBucketSelection,
} from "./platform-trends-breakdown.ts";

function breakdown(patch: Partial<PlatformTrendBreakdown>): PlatformTrendBreakdown {
  return {
    timezone: "America/Guayaquil",
    figure: "tickets_sold",
    granularity: "day",
    first_day: "2026-07-06",
    last_day: "2026-07-06",
    partial: false,
    recorded: true,
    sale_half_only: false,
    totals: [],
    organizations: [],
    events: [],
    customer_kinds: null,
    ...patch,
  };
}

const organization = (id: string, key: string, value: number, cause: "sale" | "member" | "both" | null = null) => ({
  organization_id: id,
  name: `Organization ${id}`,
  key,
  value,
  cause,
});

const event = (id: string, organizationId: string, key: string, value: number) => ({
  event_id: id,
  name: `Event ${id}`,
  organization_id: organizationId,
  organization_name: `Organization ${organizationId}`,
  key,
  value,
});

function selection(patch: Partial<TrendBucketSelection> = {}): TrendBucketSelection {
  return { figure: "tickets_sold", key: "2026-07-06", firstDay: "2026-07-06", lastDay: "2026-07-06", partial: false, ...patch };
}

// ---------------------------------------------------------------------------
// The selection to the request
// ---------------------------------------------------------------------------

test("a day is asked for by its figure and its first day", () => {
  assert.equal(
    breakdownRequestPath(selection(), "day"),
    `${PLATFORM_TREND_BREAKDOWN_API_PATH}?figure=tickets_sold&first_day=2026-07-06&granularity=day`,
  );
});

test("a week is asked for with its last day, so the stub a custom range begins with adds up too", () => {
  assert.equal(
    breakdownRequestPath(selection({ figure: "page_views", key: "2026-06-29", firstDay: "2026-06-29", lastDay: "2026-07-01" }), "week"),
    `${PLATFORM_TREND_BREAKDOWN_API_PATH}?figure=page_views&first_day=2026-06-29&granularity=week&last_day=2026-07-01`,
  );
});

test("today is asked for as a day, whatever the span is drawn in", () => {
  assert.equal(
    breakdownRequestPath(selection({ key: "2026-07-20", firstDay: "2026-07-20", lastDay: "2026-07-20", partial: true }), "week"),
    `${PLATFORM_TREND_BREAKDOWN_API_PATH}?figure=tickets_sold&first_day=2026-07-20&granularity=day`,
  );
});

test("a chart marks the selected bucket when the selection is its figure's and it draws that bucket", () => {
  const chosen = selection();
  const buckets = [{ key: "2026-07-05" }, { key: "2026-07-06" }];
  assert.equal(bucketSelectionIn(chosen, "tickets_sold", buckets), "2026-07-06");
  assert.equal(bucketSelectionIn(chosen, "page_views", buckets), null, "another figure's selection is not this chart's");
  assert.equal(bucketSelectionIn(chosen, "tickets_sold", [{ key: "2026-07-05" }]), null);
  assert.equal(bucketSelectionIn(null, "tickets_sold", buckets), null);
});

// ---------------------------------------------------------------------------
// The response to what is drawn
// ---------------------------------------------------------------------------

test("an Organization links to its operator page", () => {
  assert.equal(operatorOrganizationPath("org-1"), "/operator/organizations/org-1");
  assert.equal(operatorOrganizationPath("a/b"), "/operator/organizations/a%2Fb");
});

test("Tickets Sold: the Organizations and the Events that contributed, in the order ranked, with the total", () => {
  const view = breakdownView(
    breakdown({
      totals: [{ key: "all", value: 12 }],
      organizations: [organization("a", "all", 9), organization("b", "all", 3)],
      events: [event("x", "a", "all", 7), event("y", "b", "all", 3), event("z", "a", "all", 2)],
    }),
  );
  assert.deepEqual(view, {
    partial: false,
    salesOnly: false,
    sumOfDays: false,
    body: {
      kind: "contributors",
      unit: "count",
      groups: [
        {
          key: "all",
          currency: null,
          total: 12,
          organizations: [
            { id: "a", name: "Organization a", href: "/operator/organizations/a", organizationName: null, value: 9, cause: null },
            { id: "b", name: "Organization b", href: "/operator/organizations/b", organizationName: null, value: 3, cause: null },
          ],
          events: [
            { id: "x", name: "Event x", href: "/operator/organizations/a", organizationName: "Organization a", value: 7, cause: null },
            { id: "y", name: "Event y", href: "/operator/organizations/b", organizationName: "Organization b", value: 3, cause: null },
            { id: "z", name: "Event z", href: "/operator/organizations/a", organizationName: "Organization a", value: 2, cause: null },
          ],
        },
      ],
    },
  });
});

test("money: one group per currency, each with its own total and its own ranking, never one across them", () => {
  for (const figure of ["money_sold", "platform_fee_earned"] as const) {
    const view = breakdownView(
      breakdown({
        figure,
        totals: [
          { key: "EUR", value: 5000 },
          { key: "USD", value: 12000 },
        ],
        organizations: [organization("e", "EUR", 5000), organization("a", "USD", 9000), organization("b", "USD", 3000)],
        events: [event("p", "e", "EUR", 5000), event("x", "a", "USD", 9000), event("y", "b", "USD", 3000)],
      }),
    );
    assert.equal(view.body.kind, "contributors");
    if (view.body.kind !== "contributors") return;
    assert.equal(view.body.unit, "money");
    assert.deepEqual(
      view.body.groups.map((group) => [group.key, group.currency, group.total]),
      [
        ["EUR", "EUR", 5000],
        ["USD", "USD", 12000],
      ],
    );
    assert.deepEqual(
      view.body.groups.map((group) => group.organizations.map((row) => row.id)),
      [["e"], ["a", "b"]],
    );
    assert.deepEqual(
      view.body.groups.map((group) => group.events.map((row) => row.id)),
      [["p"], ["x", "y"]],
    );
  }
});

test("money: a currency nothing was sold in is left out beside one that has contributors", () => {
  const view = breakdownView(
    breakdown({
      figure: "money_sold",
      totals: [
        { key: "EUR", value: 0 },
        { key: "USD", value: 100 },
      ],
      organizations: [organization("a", "USD", 100)],
      events: [event("x", "a", "USD", 100)],
    }),
  );
  assert.equal(view.body.kind, "contributors");
  if (view.body.kind !== "contributors") return;
  assert.deepEqual(
    view.body.groups.map((group) => group.key),
    ["USD"],
  );
});

test("Page Views: Events alone, ranked", () => {
  const view = breakdownView(
    breakdown({
      figure: "page_views",
      totals: [{ key: "all", value: 50 }],
      events: [event("x", "a", "all", 40), event("y", "b", "all", 10)],
    }),
  );
  assert.equal(view.body.kind, "contributors");
  if (view.body.kind !== "contributors") return;
  assert.equal(view.body.unit, "count");
  const [group] = view.body.groups;
  assert.deepEqual(group?.organizations, []);
  assert.deepEqual(
    group?.events.map((row) => [row.id, row.value, row.href]),
    [
      ["x", 40, "/operator/organizations/a"],
      ["y", 10, "/operator/organizations/b"],
    ],
  );
});

test("Events Published and new Organizations: the list, with no figure beside each", () => {
  const published = breakdownView(
    breakdown({
      figure: "events_published",
      totals: [{ key: "all", value: 2 }],
      events: [event("x", "a", "all", 1), event("y", "b", "all", 1)],
    }),
  );
  assert.equal(published.body.kind, "contributors");
  if (published.body.kind !== "contributors") return;
  assert.equal(published.body.unit, "none");
  assert.equal(published.body.groups[0]?.total, 2);
  assert.deepEqual(
    published.body.groups[0]?.events.map((row) => [row.id, row.value]),
    [
      ["x", null],
      ["y", null],
    ],
  );

  const created = breakdownView(
    breakdown({
      figure: "new_organizations",
      totals: [{ key: "all", value: 1 }],
      organizations: [organization("a", "all", 1)],
    }),
  );
  assert.equal(created.body.kind, "contributors");
  if (created.body.kind !== "contributors") return;
  assert.equal(created.body.unit, "none");
  assert.deepEqual(created.body.groups[0]?.organizations, [
    { id: "a", name: "Organization a", href: "/operator/organizations/a", organizationName: null, value: null, cause: null },
  ]);
});

test("Active Organizations for a day: the list, each with what made it active", () => {
  const view = breakdownView(
    breakdown({
      figure: "active_organizations",
      totals: [{ key: "all", value: 3 }],
      organizations: [organization("a", "all", 1, "both"), organization("b", "all", 1, "member"), organization("c", "all", 1, "sale")],
    }),
  );
  assert.equal(view.salesOnly, false);
  assert.equal(view.sumOfDays, false);
  assert.equal(view.body.kind, "contributors");
  if (view.body.kind !== "contributors") return;
  assert.equal(view.body.unit, "none");
  assert.deepEqual(
    view.body.groups[0]?.organizations.map((row) => [row.id, row.cause, row.value]),
    [
      ["a", "both", null],
      ["b", "member", null],
      ["c", "sale", null],
    ],
  );
});

test("Active Organizations for a week: each with the days it was active on, the total a sum of days", () => {
  const view = breakdownView(
    breakdown({
      figure: "active_organizations",
      granularity: "week",
      first_day: "2026-06-29",
      last_day: "2026-07-05",
      totals: [{ key: "all", value: 9 }],
      organizations: [organization("a", "all", 7, "both"), organization("b", "all", 2, "sale")],
    }),
  );
  assert.equal(view.sumOfDays, true);
  assert.equal(view.body.kind, "contributors");
  if (view.body.kind !== "contributors") return;
  assert.equal(view.body.unit, "days");
  assert.deepEqual(
    view.body.groups[0]?.organizations.map((row) => [row.id, row.value]),
    [
      ["a", 7],
      ["b", 2],
    ],
  );
});

test("a sales-only breakdown says so, with a list or with none", () => {
  const listed = breakdownView(
    breakdown({
      figure: "active_organizations",
      sale_half_only: true,
      totals: [{ key: "all", value: 1 }],
      organizations: [organization("a", "all", 1, "sale")],
    }),
  );
  assert.equal(listed.salesOnly, true);
  const none = breakdownView(
    breakdown({ figure: "active_organizations", sale_half_only: true, totals: [{ key: "all", value: 0 }] }),
  );
  assert.equal(none.salesOnly, true);
  assert.deepEqual(none.body, { kind: "empty" });
});

test("today's breakdown is partial", () => {
  assert.equal(breakdownView(breakdown({ partial: true })).partial, true);
  assert.equal(breakdownView(breakdown({})).partial, false);
});

test("Active Customers: the three kinds as counts, and nobody", () => {
  const view = breakdownView(
    breakdown({
      figure: "active_customers",
      totals: [{ key: "all", value: 10 }],
      customer_kinds: { new: 4, returning: 6, returning_bought: 2 },
    }),
  );
  assert.deepEqual(view.body, { kind: "customerKinds", total: 10, new: 4, returning: 6, returningBought: 2 });
  assert.equal(view.sumOfDays, false);
});

test("Active Customers over a week says its counts are sums of days", () => {
  const view = breakdownView(
    breakdown({
      figure: "active_customers",
      granularity: "week",
      first_day: "2026-06-29",
      last_day: "2026-07-05",
      totals: [{ key: "all", value: 10 }],
      customer_kinds: { new: 4, returning: 6, returning_bought: 2 },
    }),
  );
  assert.equal(view.sumOfDays, true);
});

test("Active Customers never draws a list, whatever the response carries", () => {
  const view = breakdownView(
    breakdown({
      figure: "active_customers",
      totals: [{ key: "all", value: 1 }],
      customer_kinds: { new: 1, returning: 0, returning_bought: 0 },
      organizations: [organization("a", "all", 1)],
      events: [event("x", "a", "all", 1)],
    }),
  );
  assert.equal(view.body.kind, "customerKinds");
});

test("a bucket that was not recorded is not recorded, never a zero", () => {
  const view = breakdownView(breakdown({ figure: "active_customers", recorded: false }));
  assert.deepEqual(view.body, { kind: "notRecorded" });
});

test("new Verified Customers: a count and nothing else, whatever the response carries", () => {
  const view = breakdownView(
    breakdown({
      figure: "new_verified_customers",
      totals: [{ key: "all", value: 7 }],
      organizations: [organization("a", "all", 1)],
      events: [event("x", "a", "all", 1)],
    }),
  );
  assert.deepEqual(view.body, { kind: "count", total: 7 });
  assert.deepEqual(breakdownView(breakdown({ figure: "new_verified_customers", totals: [{ key: "all", value: 0 }] })).body, {
    kind: "count",
    total: 0,
  });
});

test("a figure with nobody contributing is empty", () => {
  for (const figure of ["tickets_sold", "money_sold", "page_views", "events_published", "new_organizations"] as const) {
    const view = breakdownView(breakdown({ figure, totals: [{ key: figure === "money_sold" ? "USD" : "all", value: 0 }] }));
    assert.deepEqual(view.body, { kind: "empty" }, figure);
  }
});
