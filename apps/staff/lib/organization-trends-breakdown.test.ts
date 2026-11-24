import assert from "node:assert/strict";
import test from "node:test";

import {
  ORGANIZATION_TREND_BREAKDOWN_API_PATH,
  organizationBreakdownRequestPath,
  organizationBreakdownView,
  type OrganizationTrendBreakdown,
} from "./organization-trends-breakdown.ts";
import { bucketSelectionIn } from "./trends-chart.ts";

function breakdown(patch: Partial<OrganizationTrendBreakdown>): OrganizationTrendBreakdown {
  return {
    timezone: "America/Guayaquil",
    figure: "tickets_sold",
    granularity: "day",
    first_day: "2026-07-02",
    last_day: "2026-07-02",
    partial: false,
    total: 6,
    events: [
      { event_id: "e1", name: "Big Fest", value: 5 },
      { event_id: "e/2", name: "Small Fest", value: 1 },
    ],
    ...patch,
  };
}

test("a day is asked for by its first day alone", () => {
  assert.equal(
    organizationBreakdownRequestPath({ figure: "page_views", key: "2026-07-02", firstDay: "2026-07-02", lastDay: "2026-07-02" }),
    `${ORGANIZATION_TREND_BREAKDOWN_API_PATH}?figure=page_views&first_day=2026-07-02`,
  );
});

test("a month bucket is asked for by its days, so a cut month reads only the days it has", () => {
  assert.equal(
    organizationBreakdownRequestPath({ figure: "takings", key: "2026-07-01", firstDay: "2026-07-01", lastDay: "2026-07-06" }),
    `${ORGANIZATION_TREND_BREAKDOWN_API_PATH}?figure=takings&first_day=2026-07-01&last_day=2026-07-06`,
  );
});

test("a selection marks a bucket on its own figure's chart alone", () => {
  const selection = { figure: "tickets_sold" as const, key: "2026-07-02", firstDay: "2026-07-02", lastDay: "2026-07-02" };
  const buckets = [{ key: "2026-07-01" }, { key: "2026-07-02" }];
  assert.equal(bucketSelectionIn(selection, "tickets_sold", buckets), "2026-07-02");
  assert.equal(bucketSelectionIn(selection, "page_views", buckets), null);
  assert.equal(bucketSelectionIn(selection, "tickets_sold", [{ key: "2026-06-01" }]), null);
  assert.equal(bucketSelectionIn(null, "tickets_sold", buckets), null);
});

test("the Events keep the backend's ranking and each links to its own page", () => {
  const view = organizationBreakdownView(breakdown({}));
  assert.equal(view.ranked, true);
  assert.equal(view.total, 6);
  assert.deepEqual(view.rows, [
    { id: "e1", name: "Big Fest", href: "/events/e1", value: 5 },
    { id: "e/2", name: "Small Fest", href: "/events/e%2F2", value: 1 },
  ]);
});

test("Events held is a list, each Event counting one, with no figure beside it", () => {
  const view = organizationBreakdownView(breakdown({ figure: "events_held", total: 2, events: [
    { event_id: "e1", name: "Big Fest", value: 1 },
    { event_id: "e2", name: "Small Fest", value: 1 },
  ] }));
  assert.equal(view.ranked, false);
  assert.deepEqual(view.rows.map((row) => row.value), [null, null]);
});
