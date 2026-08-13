import assert from "node:assert/strict";
import test from "node:test";

import { formatPriceCents } from "./events-api.ts";
import {
  drawnTicketTypes,
  formatTakings,
  formatTakingsTick,
  formatTrendsDay,
  hasSales,
  toggleTicketTypeSelection,
  trendsSeries,
  trendsYMax,
  type SalesTrends,
  type TrendsDay,
  type TrendsTicketType,
} from "./sales-trends.ts";

// A three-tier catalog in display order. VIP sorts last on purpose: the chart
// stacks in catalog order, not in the order sales arrived.
const CATALOG: TrendsTicketType[] = [
  { id: "tt_early", name: "Early Bird", sort_order: 0 },
  { id: "tt_ga", name: "General Admission", sort_order: 1 },
  { id: "tt_vip", name: "VIP", sort_order: 2 },
];

const ALL = CATALOG.map((type) => type.id);

// Three days with a silent one in the middle — the zero-filled day the API
// sends as an empty `lines` array.
const DAYS: TrendsDay[] = [
  {
    date: "2026-08-01",
    lines: [
      { ticket_type_id: "tt_early", quantity: 10, takings_cents: 50_000 },
      { ticket_type_id: "tt_ga", quantity: 4, takings_cents: 40_000 },
    ],
  },
  { date: "2026-08-02", lines: [] },
  {
    date: "2026-08-03",
    lines: [
      { ticket_type_id: "tt_ga", quantity: 3, takings_cents: 30_000 },
      { ticket_type_id: "tt_vip", quantity: 1, takings_cents: 25_000 },
    ],
  },
];

// --- chip selection -------------------------------------------------------

test("clicking a selected chip removes that Ticket Type from the drawn set", () => {
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, ALL, "tt_ga"), ["tt_early", "tt_vip"]);
});

test("clicking a deselected chip puts it back in catalog order, not click order", () => {
  const afterRemoving = toggleTicketTypeSelection(CATALOG, ALL, "tt_early");
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, afterRemoving, "tt_early"), ALL);
});

test("deselecting all but one leaves the single-Ticket-Type view", () => {
  const only = ["tt_early", "tt_ga", "tt_vip"]
    .filter((id) => id !== "tt_vip")
    .reduce((selected, id) => toggleTicketTypeSelection(CATALOG, selected, id), ALL as string[]);
  assert.deepEqual(only, ["tt_vip"]);
});

// An empty chart answers nothing, and the reader deselecting their last chip
// already has the view they were reaching for.
test("the last drawn Ticket Type cannot be deselected", () => {
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, ["tt_vip"], "tt_vip"), ["tt_vip"]);
});

test("the drawn Ticket Types keep catalog display order", () => {
  assert.deepEqual(
    drawnTicketTypes(CATALOG, ["tt_vip", "tt_early"]).map((type) => type.name),
    ["Early Bird", "VIP"],
  );
});

// --- selection to series --------------------------------------------------

test("every day keeps its slot, including the silent one", () => {
  const series = trendsSeries(DAYS, ALL, "quantity");
  assert.deepEqual(
    series.map((datum) => datum.key),
    ["2026-08-01", "2026-08-02", "2026-08-03"],
  );
  assert.equal(series[1].total, 0);
  assert.deepEqual(series[1].values, { tt_early: 0, tt_ga: 0, tt_vip: 0 });
});

test("a Ticket Type that sold nothing on a day is drawn as zero, not as a gap", () => {
  const series = trendsSeries(DAYS, ALL, "quantity");
  assert.equal(series[0].values.tt_vip, 0);
  assert.equal(series[0].values.tt_early, 10);
});

test("a deselected Ticket Type leaves both the segments and the total", () => {
  const series = trendsSeries(DAYS, ["tt_ga", "tt_vip"], "quantity");
  assert.deepEqual(series[0].values, { tt_ga: 4, tt_vip: 0 });
  // Early Bird's 10 tickets are gone from the bar, not merely hidden behind it.
  assert.equal(series[0].total, 4);
  assert.equal(Object.hasOwn(series[0].values, "tt_early"), false);
});

test("the same selection plots Takings when asked for Takings", () => {
  const series = trendsSeries(DAYS, ALL, "takings_cents");
  assert.equal(series[0].total, 90_000);
  assert.equal(series[2].values.tt_vip, 25_000);
});

// --- the Takings series ---------------------------------------------------

// The pair is the feature: one selection, one span of days, two measures. If
// the two charts could ever be handed different days or different Ticket Types,
// reading a day off one and the same day off the other would be a lie.
test("tickets and Takings are the same days and the same Ticket Types, differing only in the figure", () => {
  const selected = ["tt_early", "tt_ga"];
  const tickets = trendsSeries(DAYS, selected, "quantity");
  const takings = trendsSeries(DAYS, selected, "takings_cents");

  assert.deepEqual(
    takings.map((datum) => datum.key),
    tickets.map((datum) => datum.key),
  );
  assert.deepEqual(
    takings.map((datum) => Object.keys(datum.values)),
    tickets.map((datum) => Object.keys(datum.values)),
  );
  assert.deepEqual(tickets[0].values, { tt_early: 10, tt_ga: 4 });
  assert.deepEqual(takings[0].values, { tt_early: 50_000, tt_ga: 40_000 });
});

test("deselecting a Ticket Type takes its Takings out of the bar, not merely out of sight", () => {
  const takings = trendsSeries(DAYS, ["tt_ga", "tt_vip"], "takings_cents");
  assert.equal(takings[0].total, 40_000);
  assert.equal(Object.hasOwn(takings[0].values, "tt_early"), false);
});

test("a silent day has zero Takings rather than losing its slot", () => {
  const takings = trendsSeries(DAYS, ALL, "takings_cents");
  assert.equal(takings[1].key, "2026-08-02");
  assert.equal(takings[1].total, 0);
});

// A comp tier moves stock and earns nothing. It has to be in the tickets chart
// — the tickets were really given out — and it has to be flat in the Takings
// chart, or a free Event would look like it made money.
test("a free Ticket Type counts tickets and contributes nothing to Takings", () => {
  const catalog: TrendsTicketType[] = [
    ...CATALOG,
    { id: "tt_comp", name: "Comp", sort_order: 3 },
  ];
  const selected = catalog.map((type) => type.id);
  const days: TrendsDay[] = [
    {
      date: "2026-08-01",
      lines: [
        { ticket_type_id: "tt_ga", quantity: 2, takings_cents: 20_000 },
        { ticket_type_id: "tt_comp", quantity: 6, takings_cents: 0 },
      ],
    },
  ];

  const tickets = trendsSeries(days, selected, "quantity");
  assert.equal(tickets[0].values.tt_comp, 6);
  assert.equal(tickets[0].total, 8);

  const takings = trendsSeries(days, selected, "takings_cents");
  assert.equal(takings[0].values.tt_comp, 0);
  assert.equal(takings[0].total, 20_000);
});

test("an Event that only gave tickets away charts its tickets and no Takings", () => {
  const days: TrendsDay[] = [
    { date: "2026-08-01", lines: [{ ticket_type_id: "tt_ga", quantity: 40, takings_cents: 0 }] },
  ];
  assert.equal(trendsSeries(days, ALL, "quantity")[0].total, 40);
  assert.equal(trendsSeries(days, ALL, "takings_cents")[0].total, 0);
  // The Takings axis still draws a scale rather than collapsing to a line.
  assert.equal(trendsYMax(trendsSeries(days, ALL, "takings_cents")), 1);
});

// --- Y-max derivation -----------------------------------------------------

test("the Y max covers the tallest stack of the whole catalog", () => {
  // Aug 1 is the tallest day at 14 tickets; the axis rounds up to clear it.
  assert.equal(trendsYMax(trendsSeries(DAYS, ALL, "quantity")), 20);
});

// The point of the chips: the remaining bars get the full height of the chart.
test("deselecting the dominant Ticket Type rescales the Y axis down", () => {
  const withEarly = trendsYMax(trendsSeries(DAYS, ALL, "quantity"));
  const withoutEarly = trendsYMax(trendsSeries(DAYS, ["tt_ga", "tt_vip"], "quantity"));
  // Without Early Bird the tallest day is 4 tickets, so the axis drops to 5.
  assert.equal(withoutEarly, 5);
  assert.ok(withoutEarly < withEarly);
});

test("the Y max rounds up to a tick a person reads without counting", () => {
  assert.equal(trendsYMax([datum(0)]), 1);
  assert.equal(trendsYMax([datum(7)]), 10);
  assert.equal(trendsYMax([datum(11)]), 20);
  assert.equal(trendsYMax([datum(230)]), 250);
  assert.equal(trendsYMax([datum(6_400)]), 10_000);
});

test("an all-zero selection still draws a scale rather than collapsing", () => {
  assert.equal(trendsYMax(trendsSeries([{ date: "2026-08-02", lines: [] }], ALL, "quantity")), 1);
  assert.equal(trendsYMax([]), 1);
});

// One selection, two axes. The charts share their chips and their days, but a
// ticket count and a money figure have nothing to say to each other about
// scale, so each one is derived from its own data.
test("the Takings axis scales to Takings, independently of the tickets axis", () => {
  const tickets = trendsYMax(trendsSeries(DAYS, ALL, "quantity"));
  const takings = trendsYMax(trendsSeries(DAYS, ALL, "takings_cents"));
  assert.equal(tickets, 20); // 14 tickets on the tallest day
  assert.equal(takings, 100_000); // $900.00 on the tallest day
});

test("deselecting a Ticket Type rescales the Takings axis on its own terms", () => {
  // VIP is a small pile of tickets and most of the money. Dropping it barely
  // moves the tickets axis and collapses the Takings one, which is exactly the
  // asymmetry the two axes exist to show.
  const days: TrendsDay[] = [
    {
      date: "2026-08-01",
      lines: [
        { ticket_type_id: "tt_ga", quantity: 40, takings_cents: 40_000 },
        { ticket_type_id: "tt_vip", quantity: 4, takings_cents: 400_000 },
      ],
    },
  ];
  const selected = ["tt_ga", "tt_vip"];

  assert.equal(trendsYMax(trendsSeries(days, selected, "quantity")), 50);
  assert.equal(trendsYMax(trendsSeries(days, selected, "takings_cents")), 500_000);

  // The tickets axis hardly notices four tickets leaving; the Takings axis
  // drops by a factor of ten. Neither is derived from the other.
  assert.equal(trendsYMax(trendsSeries(days, ["tt_ga"], "quantity")), 50);
  assert.equal(trendsYMax(trendsSeries(days, ["tt_ga"], "takings_cents")), 50_000);
});

function datum(total: number) {
  return { key: "d", label: "d", values: { only: total }, total };
}

// --- Takings in the Organization's currency -------------------------------

// The claim is reuse, not resemblance: the tooltip must spell money exactly as
// the Sales tab's Net proceeds strip does, or the surfaces the note asks a
// reader to compare would not even be comparable at a glance.
test("Takings is spelled with the staff app's one money formatter", () => {
  assert.equal(formatTakings(125_000, "USD"), formatPriceCents(125_000, "USD"));
  assert.equal(formatTakings(0, "USD"), formatPriceCents(0, "USD"));
});

test("Takings is stated in the Organization's currency, not a fixed one", () => {
  assert.notEqual(formatTakings(125_000, "EUR"), formatTakings(125_000, "USD"));
  assert.equal(formatTakings(125_000, "EUR"), formatPriceCents(125_000, "EUR"));
});

// The Y axis is a fixed width so the two charts line up. A tick label that
// outgrew it would push the plot area and break the alignment the pair exists
// for, so a big day abbreviates rather than widening the axis.
test("an axis tick stays short enough for the fixed axis, however big the day", () => {
  const millionaire = 100_000_000; // $1,000,000.00
  assert.ok(formatTakingsTick(millionaire, "USD").length <= 10);
  assert.ok(formatTakingsTick(millionaire, "USD").length < formatTakings(millionaire, "USD").length);
});

test("an axis tick still names the currency", () => {
  assert.notEqual(formatTakingsTick(125_000, "EUR"), formatTakingsTick(125_000, "USD"));
});

test("a zero tick is drawn as zero money, not as a blank", () => {
  assert.ok(formatTakingsTick(0, "USD").includes("0"));
});

// --- day labels and the empty state --------------------------------------

// The API has already resolved the day into the Event's timezone. Re-reading it
// in the viewer's zone is what would slide a bar onto the wrong day.
test("a day label reads the calendar date, whatever zone the viewer is in", () => {
  const previous = process.env.TZ;
  try {
    process.env.TZ = "Pacific/Kiritimati"; // UTC+14
    assert.equal(formatTrendsDay("2026-08-01"), "Aug 1");
    process.env.TZ = "Pacific/Niue"; // UTC-11
    assert.equal(formatTrendsDay("2026-08-01"), "Aug 1");
  } finally {
    process.env.TZ = previous;
  }
});

test("an unreadable date is shown as it arrived rather than as Invalid Date", () => {
  assert.equal(formatTrendsDay("not-a-date"), "not-a-date");
});

test("an Event that has sold nothing has nothing to chart", () => {
  assert.equal(hasSales(trends([])), false);
  // An Event with External Registration sells no Ticket Sale, so it lands here
  // too — and a span of silent days is just as empty as no span at all.
  assert.equal(hasSales(trends([{ date: "2026-08-02", lines: [] }])), false);
  assert.equal(hasSales(trends(DAYS)), true);
});

function trends(days: TrendsDay[]): SalesTrends {
  return {
    timezone: "America/Guayaquil",
    currency: "USD",
    ticket_types: CATALOG,
    days,
    reversed_count: 0,
  };
}
